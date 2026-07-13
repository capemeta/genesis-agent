// Package contract 定义文件系统能力的端口接口。
package contract

// ResolveOptions 控制路径解析行为。
type ResolveOptions struct {
	Operation            string
	MustExist            bool
	AllowDirectory       bool
	RequireDirectory     bool
	PreserveFinalSymlink bool
}

// ReadOptions 控制文件读取。
type ReadOptions struct {
	MaxBytes int64
}

// WriteOptions 控制文件写入。
type WriteOptions struct {
	CreateParents bool
	Overwrite     bool
	Atomic        bool
	ExpectedHash  string
}

// ListOptions 控制目录枚举。
type ListOptions struct {
	MaxEntries int
}

// WalkOptions 控制 bounded walk。
type WalkOptions struct {
	MaxDepth       int
	MaxDirs        int
	MaxEntries     int
	MaxBytes       int64
	FollowSymlinks bool
	ExcludeDirs    []string
}

// MkdirOptions 控制目录创建。
type MkdirOptions struct {
	Parents bool
}

// RemoveOptions 控制文件删除。
type RemoveOptions struct {
	ExpectedHash string
	Recursive    bool
}
