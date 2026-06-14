package openai

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
	"github.com/rendaman0215/flakehound/internal/httpretry"
)

const (
	DefaultModel   = "gpt-5.2"
	defaultBaseURL = "https://api.openai.com/v1"
)

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
	retry      httpretry.Policy
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
	return &Client{
		apiKey:     apiKey,
		model:      model,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		retry:      httpretry.DefaultPolicy(),
	}
}

func (c *Client) Diagnose(ctx context.Context, input diagnosis.DiagnosisInput) (*diagnosis.Diagnosis, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required for provider openai")
	}

	payload := map[string]any{
		"model":        c.model,
		"instructions": "You are Flakehound, a cautious senior platform engineer. Diagnose CI failures and return only the requested JSON.",
		"input":        diagnosis.Prompt(input),
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "flakehound_diagnosis",
				"strict": true,
				"schema": diagnosis.JSONSchema(),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode OpenAI request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create OpenAI request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.retry.Do(ctx, c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("call OpenAI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError("OpenAI", resp)
	}

	var result struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode OpenAI response: %w", err)
	}
	raw := result.OutputText
	if raw == "" {
		for _, output := range result.Output {
			for _, content := range output.Content {
				if content.Type == "output_text" || content.Type == "text" {
					raw += content.Text
				}
			}
		}
	}
	return diagnosis.Parse(raw, input.Log), nil
}

func apiError(provider string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("%s API returned %s: %s", provider, resp.Status, message)
}
