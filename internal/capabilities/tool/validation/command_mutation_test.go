package validation

import (
	"testing"
)

func TestIsCommandMutatingState(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		// 只读单条命令
		{"ls -la", false},
		{"cat foo.txt", false},
		{"python -c \"import pptx; print('ok')\"", false},
		{"git status", false},
		{"git log -n 5", false},

		// 依赖安装类
		{"pip install markitdown", true},
		{"Scripts\\python.exe -m pip install markitdown[pptx]", true},
		{"npm install express", true},

		// 文件写入与重定向
		{"echo hello > output.txt", true},
		{"python script.py >> log.txt", true},
		{"mkdir unpacked", true},
		{"rm -rf test", true},

		// 复合拼接命令 (&&, ;, ||, |)
		{"python -m markitdown test.pptx | python _auto_inline_check.py", true}, // 包含 unpack/script check 或可判断
		{"ls -l && pip install foo", true},
		{"echo ok; mkdir temp", true},
		{"cat foo.txt && ls -la", false}, // 纯只读复合命令

		// Python 脚本
		{"python scripts/office/unpack.py test.pptx unpacked/", true},
	}

	for _, tt := range tests {
		got := IsCommandMutatingState(tt.cmd)
		if got != tt.want {
			t.Errorf("IsCommandMutatingState(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}
