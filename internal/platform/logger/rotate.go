package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RotateOptions 控制按日/大小滚动与保留。
type RotateOptions struct {
	Daily         bool
	MaxSizeMB     int
	RetainDays    int
	Compress      bool             // 预留；当前实现不压缩
	RotateOnStart bool             // 启动时（首次写入时）若存在旧日志，自动归档并开启全新文件
	LazyOpen      bool             // 首次 Write 前不创建/打开文件，避免未发生交互时产生空日志
	Now           func() time.Time // 可选；测试注入时钟
}

// Normalize 填充滚动默认值。
func (o *RotateOptions) Normalize() {
	if o.MaxSizeMB <= 0 {
		o.MaxSizeMB = 100
	}
	if o.RetainDays <= 0 {
		o.RetainDays = 14
	}
	// Daily 默认 true：调用方应在构造前显式设置；此处仅保证 MaxSize/Retain 有效。
}

// RotatingWriter 按日滚动，同日超大小续卷；活跃文件名为 name.log。
// 归档命名：name.YYYY-MM-DD.log，同日续卷 name.YYYY-MM-DD.N.log（N 从 1 起）。
type RotatingWriter struct {
	dir  string
	name string
	opts RotateOptions

	mu             sync.Mutex
	file           *os.File
	curDay         string
	curSize        int64
	startupPending bool
	nowFn          func() time.Time // 测试可注入
}

// NewRotatingWriter 创建滚动 Writer；name 为通道名（agent/audit/usage），不含 .log。
func NewRotatingWriter(dir, name string, opts RotateOptions) (*RotatingWriter, error) {
	opts.Normalize()
	name = strings.TrimSuffix(strings.TrimSpace(name), ".log")
	if name == "" {
		return nil, fmt.Errorf("日志通道名不能为空")
	}
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("日志目录不能为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	nowFn := time.Now
	if opts.Now != nil {
		nowFn = opts.Now
	}
	w := &RotatingWriter{
		dir:            dir,
		name:           name,
		opts:           opts,
		startupPending: opts.RotateOnStart,
		nowFn:          nowFn,
	}
	if !opts.LazyOpen {
		if err := w.openLocked(w.nowFn()); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.nowFn()
	if err := w.ensureReadyLocked(now); err != nil {
		return 0, err
	}
	n, err := w.file.Write(p)
	w.curSize += int64(n)
	return n, err
}

// Close 关闭当前文件。
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *RotatingWriter) ensureReadyLocked(now time.Time) error {
	day := now.Format("2006-01-02")
	maxBytes := int64(w.opts.MaxSizeMB) * 1024 * 1024

	if w.file == nil {
		return w.openLocked(now)
	}

	dayRoll := w.opts.Daily && w.curDay != "" && w.curDay != day
	sizeRoll := maxBytes > 0 && w.curSize >= maxBytes
	if !dayRoll && !sizeRoll {
		return nil
	}

	oldDay := w.curDay
	if err := w.file.Close(); err != nil {
		w.file = nil
		return fmt.Errorf("关闭日志文件失败: %w", err)
	}
	w.file = nil

	if err := w.archiveActive(oldDay, sizeRoll && !dayRoll); err != nil {
		return err
	}
	w.curSize = 0
	return w.openLocked(now)
}

func (w *RotatingWriter) openLocked(now time.Time) error {
	day := now.Format("2006-01-02")
	active := w.activePath()

	// 启动时（首次 Write 时）：若已存在有内容的旧活跃文件，先归档旧文件，确保本 session 写入全新的 llm.log
	if w.startupPending {
		w.startupPending = false
		if info, err := os.Stat(active); err == nil && info.Size() > 0 {
			modDay := info.ModTime().In(time.Local).Format("2006-01-02")
			if err := w.archiveActive(modDay, true); err != nil {
				return err
			}
		}
	} else if info, err := os.Stat(active); err == nil {
		// 若活跃文件来自旧日，先按日归档再新建。
		modDay := info.ModTime().In(time.Local).Format("2006-01-02")
		if w.opts.Daily && modDay != day {
			if err := w.archiveActive(modDay, false); err != nil {
				return err
			}
		}
	}

	f, err := os.OpenFile(active, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.curDay = day
	w.curSize = info.Size()

	maxBytes := int64(w.opts.MaxSizeMB) * 1024 * 1024
	if maxBytes > 0 && w.curSize >= maxBytes {
		_ = w.file.Close()
		w.file = nil
		if err := w.archiveActive(day, true); err != nil {
			return err
		}
		w.curSize = 0
		f, err = os.OpenFile(active, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("打开日志文件失败: %w", err)
		}
		w.file = f
	}

	_ = w.cleanup(now)
	return nil
}

func (w *RotatingWriter) activePath() string {
	return filepath.Join(w.dir, w.name+".log")
}

// archiveActive 归档活跃文件。sizeRoll=true 时使用 name.YYYY-MM-DD.N.log（N 从 1）；
// 日切使用 name.YYYY-MM-DD.log，若已存在则递进 N。
func (w *RotatingWriter) archiveActive(day string, sizeRoll bool) error {
	active := w.activePath()
	if _, err := os.Stat(active); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if day == "" {
		day = w.nowFn().Format("2006-01-02")
	}
	var dest string
	if sizeRoll {
		dest = w.nextIndexedArchivePath(day)
	} else {
		dest = w.nextDayArchivePath(day)
	}
	if err := os.Rename(active, dest); err != nil {
		return fmt.Errorf("归档日志失败: %w", err)
	}
	return nil
}

func (w *RotatingWriter) nextDayArchivePath(day string) string {
	base := filepath.Join(w.dir, fmt.Sprintf("%s.%s.log", w.name, day))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	return w.nextIndexedArchivePath(day)
}

func (w *RotatingWriter) nextIndexedArchivePath(day string) string {
	for n := 1; ; n++ {
		p := filepath.Join(w.dir, fmt.Sprintf("%s.%s.%d.log", w.name, day, n))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}

func (w *RotatingWriter) cleanup(now time.Time) error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	cutoff := now.AddDate(0, 0, -w.opts.RetainDays)
	prefix := w.name + "."
	active := w.name + ".log"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == active || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		rest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".log")
		parts := strings.Split(rest, ".")
		if len(parts) == 0 {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", parts[0], time.Local)
		if err != nil {
			continue
		}
		if !day.Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(w.dir, name))
	}
	return nil
}

// ListArchived 返回已归档文件名（测试用）。
func (w *RotatingWriter) ListArchived() ([]string, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	prefix := w.name + "."
	active := w.name + ".log"
	for _, entry := range entries {
		name := entry.Name()
		if name != active && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".log") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

var _ io.WriteCloser = (*RotatingWriter)(nil)
