package execution

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	execcontract "genesis-agent/internal/capabilities/execution/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
)

// TestHelperProcess 模拟交互式终端进程，实现极速、无外部依赖的跨平台交互测试。
// 仅在 GO_WANT_HELPER_PROCESS=1 时运行，模拟标准 StdIn -> StdOut 物理回显。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		// 物理回显输入行数据，用于验证 stdin 连通性
		fmt.Print(line)
	}
}

func TestLocalPTYRunner_StartAndKill(t *testing.T) {
	runner := NewLocalPTYRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionID := "local_test_session"

	// 启动当前的 TestHelperProcess 子进程
	cmd := execmodel.Command{
		Command: os.Args[0], // 指向测试二进制自身
		Shell:   execmodel.ShellSystem, // 系统原生原生执行，不加 shell 包装
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}

	err := runner.StartSession(ctx, sessionID, cmd, execcontract.RunOptions{})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// 订阅物理输出流
	outputCh, subCancel, err := runner.SubscribeOutput(ctx, sessionID)
	if err != nil {
		t.Fatalf("SubscribeOutput failed: %v", err)
	}
	defer subCancel()

	// 验证会话状态
	status, exists, err := runner.GetSessionStatus(ctx, sessionID)
	if err != nil || !exists || status != execmodel.SessionStatusRunning {
		t.Fatalf("Expected running status, got: %s (exists: %t)", status, exists)
	}

	// 模拟写入指令到交互式 Shell，使用标准换行符 \n
	err = runner.WriteStdin(ctx, sessionID, []byte("hello local helper process\n"))
	if err != nil {
		t.Fatalf("WriteStdin failed: %v", err)
	}

	// 等待输出流捕捉到数据
	var buf strings.Builder
	timeout := time.After(3 * time.Second)
	foundEcho := false

LOOP:
	for !foundEcho {
		select {
		case data, ok := <-outputCh:
			if !ok {
				break LOOP
			}
			buf.Write(data)
			if strings.Contains(buf.String(), "hello local helper process") {
				foundEcho = true
			}
		case <-timeout:
			t.Logf("Raw gathered output: %s", buf.String())
			t.Fatal("Timeout waiting for stdout echo from helper process")
		}
	}

	// 强制级联终止会话
	err = runner.KillSession(ctx, sessionID)
	if err != nil {
		t.Errorf("KillSession failed: %v", err)
	}

	// 再次验证会话是否被销毁
	status, exists, err = runner.GetSessionStatus(ctx, sessionID)
	if exists {
		t.Errorf("Session should not exist after KillSession, status: %s", status)
	}
}
