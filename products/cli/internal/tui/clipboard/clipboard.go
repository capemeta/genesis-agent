// Package clipboard 封装系统剪贴板操作，隔离 Windows / Unix 差异。
// 当前使用 github.com/atotto/clipboard，已列入项目依赖。
package clipboard

import "github.com/atotto/clipboard"

// Write 将文本写入系统剪贴板。
// 失败时返回错误，调用方负责显示 toast。
func Write(text string) error {
	return clipboard.WriteAll(text)
}

// Read 从系统剪贴板读取文本（调试/扩展用）。
func Read() (string, error) {
	return clipboard.ReadAll()
}
