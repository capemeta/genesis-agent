package model

import "time"

// EntryType 描述目录项类型。
type EntryType string

const (
	EntryTypeFile    EntryType = "file"
	EntryTypeDir     EntryType = "dir"
	EntryTypeSymlink EntryType = "symlink"
	EntryTypeOther   EntryType = "other"
)

// FileStat 是跨 backend 的文件状态快照。
type FileStat struct {
	Path        ResolvedPath `json:"path"`
	Type        EntryType    `json:"type"`
	Size        int64        `json:"size"`
	ModifiedAt  time.Time    `json:"modified_at"`
	Hash        string       `json:"hash,omitempty"`
	IsSymlink   bool         `json:"is_symlink"`
	TargetPath  string       `json:"target_path,omitempty"`
	Permissions string       `json:"permissions,omitempty"`
}

// DirEntry 是 list_dir 和 walk_dir 返回的目录项。
type DirEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       EntryType `json:"type"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// WalkError 记录 bounded walk 过程中可恢复的单路径错误。
type WalkError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// WalkOutcome 是 bounded walk 的结果。
type WalkOutcome struct {
	Root       string      `json:"root"`
	Entries    []DirEntry  `json:"entries"`
	Errors     []WalkError `json:"errors,omitempty"`
	DirsSeen   int         `json:"dirs_seen"`
	FilesSeen  int         `json:"files_seen"`
	BytesSeen  int64       `json:"bytes_seen"`
	Truncated  bool        `json:"truncated"`
	LimitCause string      `json:"limit_cause,omitempty"`
}
