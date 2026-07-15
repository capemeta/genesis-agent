package file

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	contract "genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
)

// FileLongTermMemory 本地 JSONL 文件长期记忆存储后端
type FileLongTermMemory struct {
	mu      sync.RWMutex
	baseDir string // 跨 Session 共享记忆库根目录
}

// NewFileLongTermMemory 创建本地文件长期记忆存储
func NewFileLongTermMemory(baseDir string) *FileLongTermMemory {
	return &FileLongTermMemory{
		baseDir: baseDir,
	}
}

// ltmFilePath 获取长期记忆文件路径
func (l *FileLongTermMemory) ltmFilePath() string {
	return filepath.Join(l.baseDir, "long_term_memories.jsonl")
}

// Save 保存或更新长期记忆条目（用 Map 依据 ID 去重）
func (l *FileLongTermMemory) Save(ctx context.Context, ref contract.SessionRef, entries []*domain.LongTermEntry) error {
	if len(entries) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	filePath := l.ltmFilePath()
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create ltm dir failed: %w", err)
	}

	// 1. 读取现有所有条目
	allEntries, err := l.readAllNoLock()
	if err != nil {
		return fmt.Errorf("read existing ltm failed: %w", err)
	}

	// 2. 合并去重
	entryMap := make(map[string]*domain.LongTermEntry)
	for _, old := range allEntries {
		entryMap[old.ID] = old
	}

	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if entry.ID == "" {
			entry.ID = fmt.Sprintf("ltm-%d-%s", time.Now().UnixNano(), entry.MemoryType)
		}
		if entry.TenantID == "" {
			entry.TenantID = ref.TenantID
		} else if entry.TenantID != ref.TenantID {
			return fmt.Errorf("long-term memory tenant mismatch")
		}
		entry.UpdatedAt = time.Now()
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = time.Now()
		}
		if entry.LastAccessedAt.IsZero() {
			entry.LastAccessedAt = time.Now()
		}
		if entry.Status == "" {
			entry.Status = "active"
		}
		entryMap[entry.ID] = entry
	}

	// 3. 重新写入文件
	return l.writeAllNoLock(entryMap)
}

// Search 检索长期记忆，提供作用域过滤、文本包含打分与复合加权排序
func (l *FileLongTermMemory) Search(ctx context.Context, ref contract.SessionRef, query domain.MemoryQuery) ([]*domain.LongTermEntry, error) {
	l.mu.Lock() // 需要更新最近访问时间 LastAccessedAt 并写盘，所以用写锁保护
	defer l.mu.Unlock()

	allEntries, err := l.readAllNoLock()
	if err != nil {
		return nil, fmt.Errorf("read ltm failed: %w", err)
	}

	// 1. 作用域与过滤条件判定
	var filtered []*domain.LongTermEntry
	scopeMap := make(map[string]struct{})
	for _, sc := range query.Scopes {
		key := fmt.Sprintf("%s:%s", sc.Type, sc.ID)
		scopeMap[key] = struct{}{}
	}

	for _, entry := range allEntries {
		if entry == nil || entry.TenantID != ref.TenantID {
			continue
		}
		// 隔离过滤
		if len(query.Scopes) > 0 {
			key := fmt.Sprintf("%s:%s", entry.Scope.Type, entry.Scope.ID)
			if _, matched := scopeMap[key]; !matched {
				continue
			}
		}

		// 敏感性与状态过滤
		if entry.Status != "active" {
			continue
		}
		if entry.ExpiredAt != nil && !entry.ExpiredAt.After(time.Now()) {
			continue
		}
		if query.Filters.SensitivityLevel != "" {
			if entry.SensitivityLevel != query.Filters.SensitivityLevel {
				continue
			}
		} else if entry.SensitivityLevel == "secret" || entry.SensitivityLevel == "pii" {
			continue
		}

		// 记忆类型过滤
		if len(query.Filters.MemoryTypes) > 0 {
			typeMatched := false
			for _, mt := range query.Filters.MemoryTypes {
				if entry.MemoryType == mt {
					typeMatched = true
					break
				}
			}
			if !typeMatched {
				continue
			}
		}

		// 最小重要性过滤
		if query.Filters.MinImportance > 0 && entry.Importance < query.Filters.MinImportance {
			continue
		}

		filtered = append(filtered, entry)
	}

	if len(filtered) == 0 {
		return []*domain.LongTermEntry{}, nil
	}

	// 2. 文本分词包含打分与复合排序
	type scoredEntry struct {
		entry *domain.LongTermEntry
		score float64
	}

	scoredList := make([]scoredEntry, 0, len(filtered))
	queryWords := tokenize(query.Query)

	wRel := 0.6
	wImp := 0.3
	wRec := 0.1
	if query.CompositeWeights != nil {
		wRel = query.CompositeWeights.Relevance
		wImp = query.CompositeWeights.Importance
		wRec = query.CompositeWeights.Recency
	}

	for _, entry := range filtered {
		// Relevance 计算：交集词数 / Query词数
		relevance := 0.0
		if len(queryWords) == 0 {
			relevance = 1.0
		} else {
			contentLower := strings.ToLower(entry.Content)
			matchedCount := 0
			for _, word := range queryWords {
				if strings.Contains(contentLower, word) {
					matchedCount++
					continue
				}
				// 匹配 Tags
				for _, tag := range entry.Tags {
					if strings.Contains(strings.ToLower(tag), word) {
						matchedCount++
						break
					}
				}
			}
			relevance = float64(matchedCount) / float64(len(queryWords))
		}

		// Recency 时效性：指数衰减。每 72 小时衰减一半
		recency := 1.0
		if !entry.LastAccessedAt.IsZero() {
			hoursPassed := time.Since(entry.LastAccessedAt).Hours()
			recency = math.Exp(-hoursPassed / 72.0)
		}

		// 计算最终得分
		var finalScore float64
		switch query.SortBy {
		case domain.MemorySortByRelevance:
			finalScore = relevance
		case domain.MemorySortByImportance:
			finalScore = entry.Importance
		case domain.MemorySortByRecency:
			finalScore = recency
		case domain.MemorySortByComposite:
			fallthrough
		default:
			finalScore = wRel*relevance + wImp*entry.Importance + wRec*recency
		}

		if query.MinConfidence > 0 && finalScore < query.MinConfidence {
			continue
		}

		scoredList = append(scoredList, scoredEntry{entry: entry, score: finalScore})
	}

	// 按得分降序排序
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	// 截取 TopK
	topK := query.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > len(scoredList) {
		topK = len(scoredList)
	}

	results := make([]*domain.LongTermEntry, topK)
	allEntryMap := make(map[string]*domain.LongTermEntry)
	for _, old := range allEntries {
		allEntryMap[old.ID] = old
	}

	for i := 0; i < topK; i++ {
		results[i] = scoredList[i].entry
		// 更新访问时间
		results[i].LastAccessedAt = time.Now()
		allEntryMap[results[i].ID] = results[i]
	}

	// 将被更新了 LastAccessedAt 的数据重新存盘
	if topK > 0 {
		_ = l.writeAllNoLock(allEntryMap)
	}

	return results, nil
}

// Delete 删除长期记忆
func (l *FileLongTermMemory) Delete(ctx context.Context, ref contract.SessionRef, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	allEntries, err := l.readAllNoLock()
	if err != nil {
		return fmt.Errorf("read ltm failed: %w", err)
	}

	idMap := make(map[string]struct{})
	for _, id := range ids {
		idMap[id] = struct{}{}
	}

	entryMap := make(map[string]*domain.LongTermEntry)
	for _, entry := range allEntries {
		if _, exists := idMap[entry.ID]; !exists || entry.TenantID != ref.TenantID {
			entryMap[entry.ID] = entry
		}
	}

	return l.writeAllNoLock(entryMap)
}

// readAllNoLock 读取全量日志条目（无锁）
func (l *FileLongTermMemory) readAllNoLock() ([]*domain.LongTermEntry, error) {
	filePath := l.ltmFilePath()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []*domain.LongTermEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry domain.LongTermEntry
		if err := json.Unmarshal(line, &entry); err == nil {
			entries = append(entries, &entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// writeAllNoLock 将 Map 写回本地日志（无锁）
func (l *FileLongTermMemory) writeAllNoLock(entryMap map[string]*domain.LongTermEntry) error {
	filePath := l.ltmFilePath()
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, entry := range entryMap {
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
	}

	return writer.Flush()
}

// tokenize 简单的文本分词器
func tokenize(text string) []string {
	text = strings.ToLower(text)
	// 移除非英文字母、非数字和非中文字符的特殊符号，替换为空格
	var sb strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 0x4e00 && r <= 0x9fff) || r == ' ' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}

	words := strings.Fields(sb.String())
	var cleaned []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if len(w) > 0 {
			cleaned = append(cleaned, w)
		}
	}
	return cleaned
}
