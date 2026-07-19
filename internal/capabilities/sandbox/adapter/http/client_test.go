package http

import (
	"net/http"
	"testing"
	"time"
)

func TestNewClearsCustomHTTPClientTimeout(t *testing.T) {
	client, err := New(Config{
		BaseURL: "http://127.0.0.1:18010",
		Client:  &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != 0 {
		t.Fatalf("Timeout=%s, want 0", client.httpClient.Timeout)
	}
}
