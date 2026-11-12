package modelrail_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aravinditte/benri-agent/internal/modelrail"
)

func TestMockDeterministicAndUsage(t *testing.T) {
	c := modelrail.New(modelrail.Config{}, func(m []modelrail.ChatMessage) (string, error) {
		return `{"intent":"other"}`, nil
	})
	r1, err := c.Chat(context.Background(), []modelrail.ChatMessage{{Role: "user", Content: "hello"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := c.Chat(context.Background(), []modelrail.ChatMessage{{Role: "user", Content: "hello"}}, 100)
	if r1.Content != r2.Content || !r1.Mock || r1.Usage.TotalTokens == 0 {
		t.Errorf("mock must be deterministic with usage: %+v vs %+v", r1, r2)
	}
}

type fakeTransport struct {
	status int
	body   string
	err    error
}

func (f fakeTransport) Do(req *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body)), Header: make(http.Header)}, nil
}

func TestLiveResponseParsing(t *testing.T) {
	body := `{"id":"req-1","model":"gpt-x","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	c := modelrail.New(modelrail.Config{BaseURL: "http://modelrail.test", Model: "gpt-x", Transport: fakeTransport{status: 200, body: body}}, nil)
	r, err := c.Chat(context.Background(), []modelrail.ChatMessage{{Role: "user", Content: "hi"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if r.Content != "ok" || r.Usage.TotalTokens != 5 || r.RequestID != "req-1" {
		t.Errorf("live parse wrong: %+v", r)
	}
}

func TestGatewayErrors(t *testing.T) {
	c503 := modelrail.New(modelrail.Config{BaseURL: "http://x", Transport: fakeTransport{status: 503, body: "{}"}}, nil)
	_, err := c503.Chat(context.Background(), nil, 10)
	var ge *modelrail.GatewayError
	if !errors.As(err, &ge) {
		t.Errorf("HTTP 503 must map to GATEWAY_UNAVAILABLE, got %v", err)
	}
	cTimeout := modelrail.New(modelrail.Config{BaseURL: "http://x", Timeout: time.Second, Transport: fakeTransport{err: &timeoutErr{}}}, nil)
	_, err = cTimeout.Chat(context.Background(), nil, 10)
	if !errors.As(err, &ge) {
		t.Errorf("timeout must map to GATEWAY_UNAVAILABLE, got %v", err)
	}
	if err := c503.Embeddings(context.Background(), []string{"x"}); err == nil || !strings.Contains(err.Error(), "EMBEDDINGS_UNSUPPORTED") {
		t.Errorf("embeddings must be EMBEDDINGS_UNSUPPORTED, got %v", err)
	}
}

type timeoutErr struct{}

func (e *timeoutErr) Error() string   { return "timeout" }
func (e *timeoutErr) Timeout() bool   { return true }
func (e *timeoutErr) Temporary() bool { return true }
