package file

import (
	"context"
	"testing"
	"time"

	"genesis-agent/internal/capabilities/memory/contract"
	"genesis-agent/internal/domain"
)

func TestFileUserProfileStore(t *testing.T) {
	tempDir := t.TempDir()
	store := NewFileUserProfileStore(tempDir)
	ctx := context.Background()

	tenantID := "tenant-1"
	userID := "user-123"

	// 1. Get 不存在画像时应返回默认结构
	prof, err := store.Get(ctx, tenantID, userID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if prof.UserID != userID || prof.TenantID != tenantID {
		t.Errorf("expected new profile metadata matches, got: %+v", prof)
	}
	if !prof.Builtin.MemoryOptIn {
		t.Errorf("expected MemoryOptIn default to true")
	}

	// 2. 修改并 Save 画像
	prof.Builtin.Locale = "zh-CN"
	prof.Builtin.CommunicationStyle = "casual"
	prof.CustomFields["theme"] = "dark"

	err = store.Save(ctx, tenantID, userID, prof)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 3. 再次 Get 读取验证
	profRead, err := store.Get(ctx, tenantID, userID)
	if err != nil {
		t.Fatalf("Get read failed: %v", err)
	}
	if profRead.Builtin.Locale != "zh-CN" || profRead.Builtin.CommunicationStyle != "casual" {
		t.Errorf("expected builtin fields match, got: %+v", profRead.Builtin)
	}
	if profRead.CustomFields["theme"] != "dark" {
		t.Errorf("expected custom fields match, got: %+v", profRead.CustomFields)
	}
}

func TestFileLongTermMemorySearchAndScore(t *testing.T) {
	tempDir := t.TempDir()
	ltm := NewFileLongTermMemory(tempDir)
	ctx := context.Background()

	ref := memory.SessionRef{
		TenantID:  "tenant-1",
		SessionID: "sess-abc",
		UserID:    "user-99",
	}

	// 1. 保存一组多样的记忆
	now := time.Now()
	entries := []*domain.LongTermEntry{
		{
			ID:         "entry-1",
			MemoryType: domain.MemoryTypeSemantic,
			Content:    "The project uses golang 1.22 with strict linter enabled.",
			Importance: 0.8,
			Scope: domain.MemoryScope{
				Type: domain.MemoryScopeUser,
				ID:   "user-99",
			},
			Tags:           []string{"golang", "linter"},
			LastAccessedAt: now.Add(-10 * time.Hour), // 偏旧
		},
		{
			ID:         "entry-2",
			MemoryType: domain.MemoryTypeProcedural,
			Content:    "For database deployments, we prefer Postgres rather than MySQL.",
			Importance: 0.9,
			Scope: domain.MemoryScope{
				Type: domain.MemoryScopeUser,
				ID:   "user-99",
			},
			Tags:           []string{"postgres", "database"},
			LastAccessedAt: now, // 最新
		},
		{
			ID:         "entry-3",
			MemoryType: domain.MemoryTypeSemantic,
			Content:    "Some isolated task memory not belonging to user-99.",
			Importance: 0.9,
			Scope: domain.MemoryScope{
				Type: domain.MemoryScopeUser,
				ID:   "user-other",
			},
			Tags:           []string{"isolated"},
			LastAccessedAt: now,
		},
	}

	err := ltm.Save(ctx, ref, entries)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 2. 检索：验证隔离作用域过滤（应过滤掉 entry-3）
	query := domain.MemoryQuery{
		Query: "database",
		Scopes: []domain.MemoryScope{
			{Type: domain.MemoryScopeUser, ID: "user-99"},
		},
		TopK:   5,
		SortBy: domain.MemorySortByComposite,
	}

	res, err := ltm.Search(ctx, ref, query)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// 验证结果
	if len(res) == 0 {
		t.Fatalf("should recall entries, got 0")
	}

	// 校验隔离：entry-3 绝不能在结果中
	for _, item := range res {
		if item.ID == "entry-3" {
			t.Errorf("isolated scope memory leaked into search result!")
		}
	}

	// 3. 验证打分和排序：对 "postgres database" 检索应让 entry-2 排在第一位
	queryScore := domain.MemoryQuery{
		Query: "postgres database",
		Scopes: []domain.MemoryScope{
			{Type: domain.MemoryScopeUser, ID: "user-99"},
		},
		TopK:   5,
		SortBy: domain.MemorySortByComposite,
	}

	resScore, err := ltm.Search(ctx, ref, queryScore)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resScore) == 0 {
		t.Fatalf("expected search result, got empty")
	}

	// entry-2 包含 "postgres" 和 "database"，且更近更重要，应该排首位
	if resScore[0].ID != "entry-2" {
		t.Errorf("expected entry-2 to be sorted first, got %s", resScore[0].ID)
	}

	// 4. 验证删除操作
	err = ltm.Delete(ctx, ref, []string{"entry-2"})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	resAfterDel, err := ltm.Search(ctx, ref, queryScore)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, item := range resAfterDel {
		if item.ID == "entry-2" {
			t.Errorf("entry-2 was deleted but still recalled!")
		}
	}
}

func TestFileLongTermMemorySearchEnforcesTenantExpiryAndSensitivity(t *testing.T) {
	ltm := NewFileLongTermMemory(t.TempDir())
	ctx := context.Background()
	ref := memory.SessionRef{TenantID: "tenant-a", UserID: "user-a"}
	expired := time.Now().Add(-time.Hour)
	entries := []*domain.LongTermEntry{
		{ID: "same-scope-other-tenant", TenantID: "tenant-b", Scope: domain.MemoryScope{Type: domain.MemoryScopeUser, ID: "user-a"}, MemoryType: domain.MemoryTypeSemantic, Content: "must not leak", Status: "active"},
		{ID: "expired", Scope: domain.MemoryScope{Type: domain.MemoryScopeUser, ID: "user-a"}, MemoryType: domain.MemoryTypeSemantic, Content: "must not recall", Status: "active", ExpiredAt: &expired},
		{ID: "secret", Scope: domain.MemoryScope{Type: domain.MemoryScopeUser, ID: "user-a"}, MemoryType: domain.MemoryTypeSemantic, Content: "must not inject", Status: "active", SensitivityLevel: "secret"},
		{ID: "allowed", Scope: domain.MemoryScope{Type: domain.MemoryScopeUser, ID: "user-a"}, MemoryType: domain.MemoryTypeSemantic, Content: "safe memory", Status: "active"},
	}
	// 为构造跨租户脏数据，先使用各自作用域写入。
	if err := ltm.Save(ctx, memory.SessionRef{TenantID: "tenant-b"}, entries[:1]); err != nil {
		t.Fatalf("Save tenant-b: %v", err)
	}
	if err := ltm.Save(ctx, ref, entries[1:]); err != nil {
		t.Fatalf("Save tenant-a: %v", err)
	}
	result, err := ltm.Search(ctx, ref, domain.MemoryQuery{Scopes: []domain.MemoryScope{{Type: domain.MemoryScopeUser, ID: "user-a"}}, TopK: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result) != 1 || result[0].ID != "allowed" {
		t.Fatalf("unexpected filtered result: %#v", result)
	}
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		input    string
		expected []string
	}{
		{"Hello, World! GO-1.22", []string{"hello", "world", "go", "1", "22"}},
		{"用 Go 语言编写 Web 框架", []string{"用", "go", "语言编写", "web", "框架"}},
	}

	for _, tc := range cases {
		out := tokenize(tc.input)
		if len(out) != len(tc.expected) {
			t.Errorf("tokenize(%q) len expected %d, got %d. cleaned: %+v", tc.input, len(tc.expected), len(out), out)
			continue
		}
		for i, v := range out {
			if v != tc.expected[i] {
				t.Errorf("tokenize(%q) [%d] expected %q, got %q", tc.input, i, tc.expected[i], v)
			}
		}
	}
}
