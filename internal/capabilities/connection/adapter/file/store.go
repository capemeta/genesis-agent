package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	connection "genesis-agent/internal/capabilities/connection/contract"
)

type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("connection root 不能为空")
	}
	return &Store{root: root}, nil
}

func (s *Store) CreateHTTP(_ context.Context, req connection.CreateHTTPRequestConnectionRequest) (*connection.HTTPConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = uuid.NewString()
	}
	record := &connection.HTTPConnection{
		ID:             id,
		TenantID:       req.TenantID,
		ProjectID:      req.ProjectID,
		Name:           req.Name,
		Environment:    req.Environment,
		BaseURL:        req.BaseURL,
		DefaultHeaders: cloneStringMap(req.DefaultHeaders),
		Auth:           req.Auth,
		TimeoutMS:      req.TimeoutMS,
		Retry:          cloneRetryPolicy(req.Retry),
		AllowedTools:   append([]string(nil), req.AllowedTools...),
		AllowedAgents:  append([]string(nil), req.AllowedAgents...),
		Status:         connection.StatusActive,
		Description:    req.Description,
		Tags:           append([]string(nil), req.Tags...),
		Metadata:       cloneStringMap(req.Metadata),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.write(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Store) UpdateHTTP(_ context.Context, req connection.UpdateHTTPRequestConnectionRequest) (*connection.HTTPConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.read(req.Ref)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) != "" {
		record.Name = req.Name
	}
	if req.Environment != "" {
		record.Environment = req.Environment
	}
	if strings.TrimSpace(req.BaseURL) != "" {
		record.BaseURL = req.BaseURL
	}
	if req.DefaultHeaders != nil {
		record.DefaultHeaders = cloneStringMap(req.DefaultHeaders)
	}
	if req.Auth != nil {
		record.Auth = *req.Auth
	}
	if req.TimeoutMS != nil {
		record.TimeoutMS = *req.TimeoutMS
	}
	if req.Retry != nil {
		record.Retry = cloneRetryPolicy(req.Retry)
	}
	if req.AllowedTools != nil {
		record.AllowedTools = append([]string(nil), req.AllowedTools...)
	}
	if req.AllowedAgents != nil {
		record.AllowedAgents = append([]string(nil), req.AllowedAgents...)
	}
	if req.Status != "" {
		record.Status = req.Status
	}
	if req.Description != "" {
		record.Description = req.Description
	}
	if req.Tags != nil {
		record.Tags = append([]string(nil), req.Tags...)
	}
	if req.Metadata != nil {
		record.Metadata = cloneStringMap(req.Metadata)
	}
	record.UpdatedAt = time.Now()
	if err := s.write(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Store) GetHTTP(_ context.Context, ref connection.Ref) (*connection.HTTPConnection, error) {
	return s.read(ref)
}

func (s *Store) DeleteHTTP(_ context.Context, ref connection.Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(ref)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) ListHTTP(_ context.Context, filter connection.Filter) ([]*connection.HTTPConnection, error) {
	tenantID := strings.TrimSpace(filter.TenantID)
	if tenantID == "" {
		tenantID = "dev"
	}
	dir := filepath.Join(s.root, "connections", "tenants", tenantID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []*connection.HTTPConnection{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]*connection.HTTPConnection, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var record connection.HTTPConnection
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		if filter.ProjectID != "" && record.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Environment != "" && record.Environment != filter.Environment {
			continue
		}
		if filter.Status != "" && record.Status != filter.Status {
			continue
		}
		item := record
		result = append(result, &item)
	}
	return result, nil
}

func (s *Store) read(ref connection.Ref) (*connection.HTTPConnection, error) {
	data, err := os.ReadFile(s.path(ref))
	if err != nil {
		return nil, err
	}
	var record connection.HTTPConnection
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) write(record *connection.HTTPConnection) error {
	path := s.path(connection.Ref{TenantID: record.TenantID, ID: record.ID})
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (s *Store) path(ref connection.Ref) string {
	return filepath.Join(s.root, "connections", "tenants", ref.TenantID, ref.ID+".json")
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneRetryPolicy(input *connection.RetryPolicy) *connection.RetryPolicy {
	if input == nil {
		return nil
	}
	out := *input
	out.RetryStatusCodes = append([]int(nil), input.RetryStatusCodes...)
	out.RetryMethods = append([]string(nil), input.RetryMethods...)
	return &out
}
