package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"genesis-agent/internal/capabilities/web/contract"
	"genesis-agent/internal/capabilities/web/service"
	platformhttp "genesis-agent/internal/platform/httpclient"
)

func TestFetcher_Fetch(t *testing.T) {
	service.AllowLoopbackForTest = true
	defer func() { service.AllowLoopbackForTest = false }()

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("<html><body>Content</body></html>"))
	}))
	defer server.Close()

	f := NewFetcher()
	// Override inner client with server client to reach loopback safely in this test
	f.client = platformhttp.New(platformhttp.WithHTTPClient(server.Client()))

	req := contract.FetchRequest{
		URL:      server.URL,
		MaxBytes: 1024,
		Timeout:  3 * time.Second,
	}

	doc, err := f.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if doc.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", doc.StatusCode)
	}

	if !strings.Contains(doc.ContentType, "text/html") {
		t.Errorf("expected html content type, got %s", doc.ContentType)
	}

	if string(doc.Body) != "<html><body>Content</body></html>" {
		t.Errorf("unexpected body content: %s", string(doc.Body))
	}
}

func TestFetcher_Redirect(t *testing.T) {
	service.AllowLoopbackForTest = true
	defer func() { service.AllowLoopbackForTest = false }()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/redirect" {
			rw.Header().Set("Location", "/target")
			rw.WriteHeader(http.StatusFound)
			return
		}
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte("TargetReached"))
	}))
	defer server.Close()

	f := NewFetcher()
	f.client = platformhttp.New(platformhttp.WithHTTPClient(server.Client()))

	req := contract.FetchRequest{
		URL:             server.URL + "/redirect",
		MaxBytes:        1024,
		FollowRedirects: true,
	}

	doc, err := f.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("Fetch redirect failed: %v", err)
	}

	if string(doc.Body) != "TargetReached" {
		t.Errorf("expected redirected target body 'TargetReached', got %q", string(doc.Body))
	}
}
