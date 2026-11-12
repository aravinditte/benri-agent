// Package modelrail is the typed ModelRail gateway client (SPEC §10.1),
// ported from v2, plus the deterministic mock mode.
//
// Embeddings capability spike result (2026-11-12, re-confirmed for v3): the
// sibling ModelRail codebase contains NO embeddings endpoint (chat/completions
// only). Dense retrieval is therefore DISABLED; Benri ships lexical-only
// retrieval. Never silently bypassed.
package modelrail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResult struct {
	Content   string
	Usage     Usage
	RequestID string
	Model     string
	LatencyMS int64
	Mock      bool
}

// GatewayError is the typed GATEWAY_UNAVAILABLE failure (SPEC §10.1).
type GatewayError struct{ Msg string }

func (e *GatewayError) Error() string { return "GATEWAY_UNAVAILABLE: " + e.Msg }
func (e *GatewayError) Code() string  { return "GATEWAY_UNAVAILABLE" }

// MockResponder produces deterministic mock content (MOCK_LLM=1).
type MockResponder func(messages []ChatMessage) (string, error)

// Transport lets tests inject a fake HTTP round tripper.
type Transport interface {
	Do(req *http.Request) (*http.Response, error)
}

type Config struct {
	BaseURL          string
	APIKey           string
	Model            string
	Timeout          time.Duration
	MaxResponseBytes int64
	Transport        Transport
}

type Client struct {
	cfg    Config
	mock   bool
	mockFn MockResponder
}

func New(cfg Config, mockFn MockResponder) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 1 << 20
	}
	return &Client{cfg: cfg, mock: cfg.BaseURL == "", mockFn: mockFn}
}

func (c *Client) Mock() bool { return c.mock }

func (c *Client) Chat(ctx context.Context, messages []ChatMessage, maxTokens int) (*ChatResult, error) {
	started := time.Now()
	if c.mock {
		if c.mockFn == nil {
			return nil, &GatewayError{Msg: "MOCK_LLM enabled but no mock responder configured"}
		}
		content, err := c.mockFn(messages)
		if err != nil {
			return nil, &GatewayError{Msg: "mock responder failed"}
		}
		prompt, completion := 0, len([]rune(content))/2
		for _, m := range messages {
			prompt += len([]rune(m.Content)) / 2
		}
		return &ChatResult{Content: content, Usage: Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}, Model: "mock-deterministic-v1", LatencyMS: time.Since(started).Milliseconds(), Mock: true}, nil
	}
	if maxTokens == 0 {
		maxTokens = 1200
	}
	body, _ := json.Marshal(map[string]any{"model": c.cfg.Model, "messages": messages, "max_tokens": maxTokens})
	url := c.cfg.BaseURL
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, &GatewayError{Msg: err.Error()}
	}
	req.Header.Set("content-type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("authorization", "Bearer "+c.cfg.APIKey)
	}
	transport := c.cfg.Transport
	if transport == nil {
		transport = &http.Client{Timeout: c.cfg.Timeout}
	}
	res, err := transport.Do(req)
	if err != nil {
		return nil, &GatewayError{Msg: "ModelRail request failed: " + errName(err)}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &GatewayError{Msg: fmt.Sprintf("ModelRail HTTP %d", res.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, c.cfg.MaxResponseBytes))
	if err != nil {
		return nil, &GatewayError{Msg: "ModelRail response read failed"}
	}
	var data struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || len(data.Choices) == 0 || data.Choices[0].Message.Content == "" {
		return nil, &GatewayError{Msg: "ModelRail response missing choices[0].message.content"}
	}
	usage := Usage{}
	if data.Usage != nil {
		usage = *data.Usage
	}
	model := data.Model
	if model == "" {
		model = c.cfg.Model
	}
	return &ChatResult{Content: data.Choices[0].Message.Content, Usage: usage, RequestID: data.ID, Model: model, LatencyMS: time.Since(started).Milliseconds()}, nil
}

// Embeddings is NOT available: the capability spike found no endpoint in
// ModelRail, so dense retrieval is disabled (SPEC §8.3/§19.1).
func (c *Client) Embeddings(_ context.Context, _ []string) error {
	return &GatewayError{Msg: "EMBEDDINGS_UNSUPPORTED: ModelRail exposes no embeddings endpoint (capability spike 2026-11-12); dense retrieval disabled"}
}

func errName(err error) string {
	var ue interface{ Timeout() bool }
	if errors.As(err, &ue) && ue.Timeout() {
		return "timeout"
	}
	return "transport error"
}
