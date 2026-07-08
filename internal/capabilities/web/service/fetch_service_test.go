package service

import (
	"context"
	"net"
	"strings"
	"testing"

	"genesis-agent/internal/capabilities/web/contract"
)

type mockFetcher struct {
	fn func(req contract.FetchRequest) (contract.FetchedDocument, error)
}

func (m *mockFetcher) Fetch(ctx context.Context, req contract.FetchRequest) (contract.FetchedDocument, error) {
	return m.fn(req)
}

type mockExtractor struct {
	fn func(doc contract.FetchedDocument, req contract.FetchRequest) (contract.ExtractedDocument, error)
}

func (m *mockExtractor) Extract(ctx context.Context, doc contract.FetchedDocument, req contract.FetchRequest) (contract.ExtractedDocument, error) {
	return m.fn(doc, req)
}

type mockPolicy struct {
	fnSearch func(req contract.SearchRequest) (contract.PolicyDecision, error)
	fnFetch  func(req contract.FetchRequest) (contract.PolicyDecision, error)
}

func (m *mockPolicy) AuthorizeSearch(ctx context.Context, req contract.SearchRequest) (contract.PolicyDecision, error) {
	if m.fnSearch == nil {
		return contract.PolicyDecision{Allowed: true}, nil
	}
	return m.fnSearch(req)
}

func (m *mockPolicy) AuthorizeFetch(ctx context.Context, req contract.FetchRequest) (contract.PolicyDecision, error) {
	if m.fnFetch == nil {
		return contract.PolicyDecision{Allowed: true}, nil
	}
	return m.fnFetch(req)
}

func TestFetchService_Fetch(t *testing.T) {
	f := &mockFetcher{
		fn: func(req contract.FetchRequest) (contract.FetchedDocument, error) {
			return contract.FetchedDocument{
				URL:         "https://example.com/page",
				StatusCode:  200,
				ContentType: "text/html",
				Body:        []byte("<html><body>Hello World</body></html>"),
			}, nil
		},
	}

	ext := &mockExtractor{
		fn: func(doc contract.FetchedDocument, req contract.FetchRequest) (contract.ExtractedDocument, error) {
			return contract.ExtractedDocument{
				Title:       "Test Page",
				Description: "Desc",
				Markdown:    "Hello World",
				Text:        "Hello World",
			}, nil
		},
	}

	pol := &mockPolicy{
		fnFetch: func(req contract.FetchRequest) (contract.PolicyDecision, error) {
			return contract.PolicyDecision{Allowed: true, Reason: ""}, nil
		},
	}

	svc := NewFetchService(f, ext, pol, nil)
	req := contract.FetchRequest{
		URL:      "https://example.com/page",
		MaxBytes: 1024,
		MaxChars: 100,
	}

	res, err := svc.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("fetch service failed: %v", err)
	}

	if res.Title != "Test Page" {
		t.Errorf("expected Title 'Test Page', got %q", res.Title)
	}

	if res.Markdown != "Hello World" {
		t.Errorf("expected Markdown 'Hello World', got %q", res.Markdown)
	}
}

func TestFetchService_Fetch_BlockedByPolicy(t *testing.T) {
	f := &mockFetcher{}
	ext := &mockExtractor{}
	pol := &mockPolicy{
		fnFetch: func(req contract.FetchRequest) (contract.PolicyDecision, error) {
			return contract.PolicyDecision{Allowed: false, Reason: "blocked domain"}, nil
		},
	}

	svc := NewFetchService(f, ext, pol, nil)
	req := contract.FetchRequest{
		URL: "https://blocked.com",
	}

	_, err := svc.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from blocked policy but got nil")
	}

	if !strings.Contains(err.Error(), "fetch blocked by policy") {
		t.Errorf("expected 'fetch blocked by policy' error, got %v", err)
	}
}

func TestFetchService_IsIPSafe(t *testing.T) {
	tests := []struct {
		ip   string
		safe bool
	}{
		{"8.8.8.8", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"172.16.5.5", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"100.64.0.1", false},
		{"224.0.0.1", false},
		{"0.0.0.0", false},
		{"0.1.2.3", false},
		{"::1", false},
		{"fc00::1", false},
		{"fe80::1", false},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP %s", tt.ip)
		}
		if got := IsIPSafe(ip); got != tt.safe {
			t.Errorf("IsIPSafe(%s) = %v; want %v", tt.ip, got, tt.safe)
		}
	}
}

func TestFetchService_SSRFBlock(t *testing.T) {
	f := &mockFetcher{}
	ext := &mockExtractor{}
	pol := &mockPolicy{}

	svc := NewFetchService(f, ext, pol, nil)
	req := contract.FetchRequest{
		URL: "http://127.0.0.1/admin",
	}

	_, err := svc.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for loopback URL, got nil")
	}
	if !strings.Contains(err.Error(), "unsafe IP address resolved") && !strings.Contains(err.Error(), "failed to resolve host") {
		t.Errorf("expected unsafe IP error, got %v", err)
	}
}

func TestFetchService_SchemeBlock(t *testing.T) {
	f := &mockFetcher{}
	ext := &mockExtractor{}
	pol := &mockPolicy{}

	svc := NewFetchService(f, ext, pol, nil)
	req := contract.FetchRequest{
		URL: "ftp://example.com/file",
	}

	_, err := svc.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for non-http scheme, got nil")
	}
	if !strings.Contains(err.Error(), "forbidden scheme") {
		t.Errorf("expected forbidden scheme error, got %v", err)
	}
}

func TestFetchService_CredentialsBlock(t *testing.T) {
	f := &mockFetcher{}
	ext := &mockExtractor{}
	pol := &mockPolicy{}

	svc := NewFetchService(f, ext, pol, nil)
	req := contract.FetchRequest{
		URL: "http://admin:password@example.com",
	}

	_, err := svc.Fetch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for credentials URL, got nil")
	}
	if !strings.Contains(err.Error(), "URL credentials are not allowed") {
		t.Errorf("expected credentials error, got %v", err)
	}
}
