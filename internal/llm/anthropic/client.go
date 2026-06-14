package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rendaman0215/flakehound/internal/diagnosis"
)

const (
	DefaultModel   = "claude-sonnet-4-5"
	defaultBaseURL = "https://api.anthropic.com/v1"
)

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func New(apiKey, model string) *Client {
	return NewWithOptions(apiKey, model, defaultBaseURL, &http.Client{Timeout: 90 * time.Second})
}

func NewWithOptions(apiKey, model, baseURL string, httpClient *http.Client) *Client {
	if model == "" {
		model = DefaultModel
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &Client{apiKey: apiKey, model: model, baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *Client) Diagnose(ctx context.Context, input diagnosis.DiagnosisInput) (*diagnosis.Diagnosis, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required for provider anthropic")
	}

	payload := map[string]any{
		"model":      c.model,
		"max_tokens": 1400,
		"system":     "You are Flakehound, a cautious senior platform engineer. Return only one JSON object matching the schema described by the user. Do not wrap it in Markdown.",
		"messages": []map[string]string{
			{"role": "user", "content": diagnosis.Prompt(input)},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Anthropic request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Anthropic request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call Anthropic: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError("Anthropic", resp)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Anthropic response: %w", err)
	}
	var raw strings.Builder
	for _, content := range result.Content {
		if content.Type == "text" {
			raw.WriteString(content.Text)
		}
	}
	return diagnosis.Parse(raw.String()), nil
}

func apiError(provider string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("%s API returned %s: %s", provider, resp.Status, message)
}
