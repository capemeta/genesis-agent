package tui

import (
	"context"
	"io"
	"os"

	"golang.org/x/term"
)

// BridgeRawTerminal 临时使当前终端进入 RAW 模式，物理桥接宿主 Stdin 和 Stdout。
// 此接管机制提供 0 延迟、100% 原生的终端体验，支持 Tab 补全及 Vim 输入。
func BridgeRawTerminal(ctx context.Context, stdinWriter io.Writer, stdoutReader io.Reader) error {
	// 1. 进入原始（Raw）输入模式以拦截所有控制字符（如 Ctrl+C、向上方向键等）
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer func() {
		if closer, ok := stdinWriter.(io.Closer); ok {
			_ = closer.Close()
		}
		if closer, ok := stdoutReader.(io.Closer); ok {
			_ = closer.Close()
		}
		_ = term.Restore(int(os.Stdin.Fd()), oldState)
	}()

	errCh := make(chan error, 2)

	// 2. 双向管道物理拷贝
	go func() {
		_, copyErr := io.Copy(stdinWriter, os.Stdin)
		errCh <- copyErr
	}()

	go func() {
		_, copyErr := io.Copy(os.Stdout, stdoutReader)
		errCh <- copyErr
	}()

	// 3. 阻塞等待其中一端关闭（例如输入 exit ）或 context 结束
	select {
	case <-ctx.Done():
		return ctx.Err()
	case copyErr := <-errCh:
		return copyErr
	}
}
