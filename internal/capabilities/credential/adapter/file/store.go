package file

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	credential "genesis-agent/internal/capabilities/credential/contract"
)

const (
	algorithmAESGCM = "AES-256-GCM"
)

type Store struct {
	root  string
	key   []byte
	keyID string
	mu    sync.Mutex
}

type encryptedCredential struct {
	Meta       credential.CredentialMeta `json:"meta"`
	Ciphertext []byte                    `json:"ciphertext"`
	Nonce      []byte                    `json:"nonce"`
	Algorithm  string                    `json:"algorithm"`
	KeyID      string                    `json:"key_id"`
}

func New(root string, masterKey string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("credential root 不能为空")
	}
	if strings.TrimSpace(masterKey) == "" {
		return nil, fmt.Errorf("master key 不能为空")
	}
	sum := sha256.Sum256([]byte(masterKey))
	keyID := fmt.Sprintf("sha256:%x", sum[:8])
	return &Store{root: root, key: sum[:], keyID: keyID}, nil
}

func (s *Store) Create(_ context.Context, req credential.CreateCredentialRequest) (*credential.CredentialMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = uuid.NewString()
	}
	meta := credential.CredentialMeta{
		ID:          id,
		TenantID:    req.TenantID,
		ProjectID:   req.ProjectID,
		Name:        req.Name,
		Type:        req.Type,
		Status:      credential.CredentialStatusActive,
		Version:     1,
		Description: req.Description,
		Tags:        append([]string(nil), req.Tags...),
		Metadata:    cloneStringMap(req.Metadata),
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   req.ExpiresAt,
	}
	record, err := s.encrypt(meta, req.Secret)
	if err != nil {
		return nil, err
	}
	if err := s.writeRecord(record); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Store) Update(_ context.Context, req credential.UpdateCredentialRequest) (*credential.CredentialMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.readRecord(req.Ref)
	if err != nil {
		return nil, err
	}
	secret, err := s.decrypt(record)
	if err != nil {
		return nil, err
	}
	if req.Secret != "" {
		secret = req.Secret
		now := time.Now()
		record.Meta.Version++
		record.Meta.RotatedAt = &now
	}
	if req.Description != "" {
		record.Meta.Description = req.Description
	}
	if req.Tags != nil {
		record.Meta.Tags = append([]string(nil), req.Tags...)
	}
	if req.Metadata != nil {
		record.Meta.Metadata = cloneStringMap(req.Metadata)
	}
	if req.ExpiresAt != nil {
		record.Meta.ExpiresAt = req.ExpiresAt
	}
	record.Meta.UpdatedAt = time.Now()
	record, err = s.encrypt(record.Meta, secret)
	if err != nil {
		return nil, err
	}
	if err := s.writeRecord(record); err != nil {
		return nil, err
	}
	meta := record.Meta
	return &meta, nil
}

func (s *Store) GetMeta(_ context.Context, ref credential.CredentialRef) (*credential.CredentialMeta, error) {
	record, err := s.readRecord(ref)
	if err != nil {
		return nil, err
	}
	meta := record.Meta
	return &meta, nil
}

func (s *Store) Resolve(_ context.Context, ref credential.CredentialRef, _ credential.ResolvePurpose) (*credential.ResolvedCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.readRecord(ref)
	if err != nil {
		return nil, err
	}
	if record.Meta.Status != credential.CredentialStatusActive {
		return nil, fmt.Errorf("credential %s 已禁用", ref.ID)
	}
	secret, err := s.decrypt(record)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	record.Meta.LastUsedAt = &now
	record.Meta.UpdatedAt = now
	if err := s.writeRecord(record); err != nil {
		return nil, err
	}
	return &credential.ResolvedCredential{Meta: record.Meta, Secret: secret}, nil
}

func (s *Store) Delete(_ context.Context, ref credential.CredentialRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(ref)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) List(_ context.Context, filter credential.CredentialFilter) ([]*credential.CredentialMeta, error) {
	tenantID := strings.TrimSpace(filter.TenantID)
	if tenantID == "" {
		tenantID = "dev"
	}
	dir := filepath.Join(s.root, "credentials", "tenants", tenantID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []*credential.CredentialMeta{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]*credential.CredentialMeta, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var record encryptedCredential
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		if filter.ProjectID != "" && record.Meta.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Status != "" && record.Meta.Status != filter.Status {
			continue
		}
		meta := record.Meta
		result = append(result, &meta)
	}
	return result, nil
}

func (s *Store) encrypt(meta credential.CredentialMeta, secret string) (*encryptedCredential, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("创建 AES cipher 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 失败: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	aad := []byte(meta.TenantID + ":" + meta.ID + ":" + meta.Name)
	ciphertext := gcm.Seal(nil, nonce, []byte(secret), aad)
	return &encryptedCredential{
		Meta:       meta,
		Ciphertext: ciphertext,
		Nonce:      nonce,
		Algorithm:  algorithmAESGCM,
		KeyID:      s.keyID,
	}, nil
}

func (s *Store) decrypt(record *encryptedCredential) (string, error) {
	if record.Algorithm != algorithmAESGCM {
		return "", fmt.Errorf("不支持的加密算法: %s", record.Algorithm)
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("创建 AES cipher 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("创建 GCM 失败: %w", err)
	}
	aad := []byte(record.Meta.TenantID + ":" + record.Meta.ID + ":" + record.Meta.Name)
	plaintext, err := gcm.Open(nil, record.Nonce, record.Ciphertext, aad)
	if err != nil {
		return "", fmt.Errorf("解密 credential 失败: %w", err)
	}
	return string(plaintext), nil
}

func (s *Store) readRecord(ref credential.CredentialRef) (*encryptedCredential, error) {
	data, err := os.ReadFile(s.path(ref))
	if err != nil {
		return nil, err
	}
	var record encryptedCredential
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) writeRecord(record *encryptedCredential) error {
	path := s.path(credential.CredentialRef{TenantID: record.Meta.TenantID, ID: record.Meta.ID})
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (s *Store) path(ref credential.CredentialRef) string {
	return filepath.Join(s.root, "credentials", "tenants", ref.TenantID, ref.ID+".json")
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
