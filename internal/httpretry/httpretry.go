package httpretry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const drainLimit = 64 << 10

type SleepFunc func(context.Context, time.Duration) error

type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Sleep       SleepFunc
	Now         func() time.Time
}

func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts: 3,
		BaseDelay:   250 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Sleep:       sleep,
		Now:         time.Now,
	}
}

func (p Policy) Do(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	p = p.withDefaults()

	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		attemptReq, retryableBody, err := requestForAttempt(req, ctx, attempt)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(attemptReq)
		if err != nil {
			return nil, err
		}
		if !isTransient(resp.StatusCode) || attempt == p.MaxAttempts || !retryableBody {
			return resp, nil
		}

		closeResponse(resp)
		if err := p.Sleep(ctx, p.delay(resp, attempt)); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("HTTP retry attempts exhausted")
}

func (p Policy) withDefaults() Policy {
	defaults := DefaultPolicy()
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = defaults.MaxAttempts
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = defaults.BaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = defaults.MaxDelay
	}
	if p.Sleep == nil {
		p.Sleep = defaults.Sleep
	}
	if p.Now == nil {
		p.Now = defaults.Now
	}
	return p
}

func (p Policy) delay(resp *http.Response, attempt int) time.Duration {
	delay := p.BaseDelay
	for i := 1; i < attempt && delay < p.MaxDelay; i++ {
		if delay > p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}

	if retryAfter, ok := parseRetryAfter(resp.Header.Get("Retry-After"), p.Now()); ok {
		delay = retryAfter
	}
	if delay > p.MaxDelay {
		return p.MaxDelay
	}
	return delay
}

func requestForAttempt(req *http.Request, ctx context.Context, attempt int) (*http.Request, bool, error) {
	if attempt == 1 {
		return req, req.Body == nil || req.GetBody != nil, nil
	}
	if req.Body == nil {
		return req.Clone(ctx), true, nil
	}
	if req.GetBody == nil {
		return nil, false, fmt.Errorf("recreate HTTP request body for retry: GetBody is unavailable")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, false, fmt.Errorf("recreate HTTP request body for retry: %w", err)
	}
	retryReq := req.Clone(ctx)
	retryReq.Body = body
	return retryReq, true, nil
}

func isTransient(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500 && statusCode <= 599
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		if seconds > (1<<63-1)/int64(time.Second) {
			return time.Duration(1<<63 - 1), true
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if delay := when.Sub(now); delay > 0 {
		return delay, true
	}
	return 0, true
}

func closeResponse(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
	_ = resp.Body.Close()
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
