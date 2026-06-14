package httpretry

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestDoHonorsRetryAfterAndClosesResponse(t *testing.T) {
	firstBody := &trackingBody{Reader: bytes.NewBufferString("retry")}
	attempts := 0
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"2"}},
				Body:       firstBody,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
		}, nil
	})}

	var slept time.Duration
	policy := DefaultPolicy()
	policy.Sleep = func(_ context.Context, delay time.Duration) error {
		slept = delay
		return nil
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.test", bytes.NewReader([]byte("request")))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := policy.Do(context.Background(), client, req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if attempts != 2 || slept != 2*time.Second || !firstBody.closed {
		t.Fatalf("attempts=%d slept=%s first body closed=%t", attempts, slept, firstBody.closed)
	}
}

func TestDoStopsWhileWaitingWhenContextCanceled(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString("retry")),
		}, nil
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	policy := DefaultPolicy()
	policy.Sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err = policy.Do(ctx, client, req)
	if err != context.Canceled {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}
