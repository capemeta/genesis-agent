package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoJSONAndAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("X-Request-ID"); got == "" {
			t.Fatalf("expected request id header")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"method": r.Method,
			"body":   string(body),
		})
	}))
	defer server.Close()

	client := New()
	resp, err := client.Do(context.Background(), &Request{
		Method:  http.MethodPost,
		BaseURL: server.URL,
		Path:    "/chat",
		Auth: &AuthConfig{
			Type:  AuthTypeBearerToken,
			Token: "test-token",
		},
		Body: map[string]string{"hello": "world"},
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}

	var payload map[string]string
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["method"] != http.MethodPost {
		t.Fatalf("unexpected method: %q", payload["method"])
	}
	if !strings.Contains(payload["body"], `"hello":"world"`) {
		t.Fatalf("unexpected body: %q", payload["body"])
	}
}

func TestDoRetry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "retry me", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := New(WithConfig(Config{
		DefaultTimeout: 2 * time.Second,
		Retry: RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     10 * time.Millisecond,
			Multiplier:     1,
		},
	}))

	resp, err := client.Do(context.Background(), &Request{
		Method:  http.MethodGet,
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if string(resp.Body) != "ok" {
		t.Fatalf("unexpected body: %q", string(resp.Body))
	}
	if attempts.Load() != 3 {
		t.Fatalf("unexpected attempts: %d", attempts.Load())
	}
}

func TestDoTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(WithConfig(Config{
		DefaultTimeout:        20 * time.Millisecond,
		ResponseHeaderTimeout: 20 * time.Millisecond,
		Retry: RetryPolicy{
			MaxAttempts: 1,
		},
	}))

	_, err := client.Do(context.Background(), &Request{
		Method:  http.MethodGet,
		BaseURL: server.URL,
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}

	var httpErr *Error
	if !errorsAs(err, &httpErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if httpErr.Kind != ErrorKindTimeout {
		t.Fatalf("unexpected error kind: %s", httpErr.Kind)
	}
}

func TestDoResponseBodyLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("123456"))
	}))
	defer server.Close()

	client := New()
	_, err := client.Do(context.Background(), &Request{
		Method:               http.MethodGet,
		BaseURL:              server.URL,
		MaxResponseBodyBytes: 4,
	})
	if err == nil {
		t.Fatalf("expected too large error")
	}

	var httpErr *Error
	if !errorsAs(err, &httpErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if httpErr.Kind != ErrorKindTooLarge {
		t.Fatalf("unexpected error kind: %s", httpErr.Kind)
	}
}

func TestStreamSSE(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "id: 1\n")
		_, _ = io.WriteString(w, "event: message\n")
		_, _ = io.WriteString(w, "data: hello\n")
		_, _ = io.WriteString(w, "data: world\n\n")
	}))
	defer server.Close()

	client := New()
	stream, err := client.StreamSSE(context.Background(), &Request{
		Method:  http.MethodGet,
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("StreamSSE() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if event.ID != "1" || event.Event != "message" || string(event.Data) != "hello\nworld" {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func errorsAs(err error, target **Error) bool {
	if err == nil {
		return false
	}
	value, ok := err.(*Error)
	if !ok {
		return false
	}
	*target = value
	return true
}
