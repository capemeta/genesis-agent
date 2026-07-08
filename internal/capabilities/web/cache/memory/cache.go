package memory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"genesis-agent/internal/capabilities/web/contract"
)

type Cache struct {
	mu           sync.RWMutex
	searchCache  map[string]contract.SearchResult
	fetchCache   map[string]contract.FetchResult
}

func NewCache() *Cache {
	return &Cache{
		searchCache: make(map[string]contract.SearchResult),
		fetchCache:  make(map[string]contract.FetchResult),
	}
}

func (c *Cache) GetSearch(ctx context.Context, req contract.SearchRequest) (contract.SearchResult, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key, err := searchKey(req)
	if err != nil {
		return contract.SearchResult{}, false, err
	}

	res, found := c.searchCache[key]
	return res, found, nil
}

func (c *Cache) SetSearch(ctx context.Context, req contract.SearchRequest, res contract.SearchResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key, err := searchKey(req)
	if err != nil {
		return err
	}

	c.searchCache[key] = res
	return nil
}

func (c *Cache) GetFetch(ctx context.Context, req contract.FetchRequest) (contract.FetchResult, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key, err := fetchKey(req)
	if err != nil {
		return contract.FetchResult{}, false, err
	}

	res, found := c.fetchCache[key]
	return res, found, nil
}

func (c *Cache) SetFetch(ctx context.Context, req contract.FetchRequest, res contract.FetchResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key, err := fetchKey(req)
	if err != nil {
		return err
	}

	c.fetchCache[key] = res
	return nil
}

func (c *Cache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.searchCache = make(map[string]contract.SearchResult)
	c.fetchCache = make(map[string]contract.FetchResult)
	return nil
}

func searchKey(req contract.SearchRequest) (string, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func fetchKey(req contract.FetchRequest) (string, error) {
	// Exclude dynamic prompt or other parameters if needed, but keeping them in the key is safer.
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
