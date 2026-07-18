package builtin

import (
	"context"
	"testing"

	platformhttp "genesis-agent/internal/platform/httpclient"
)

func TestBuiltinToolsRejectUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "current_time", run: func() error {
			_, err := NewCurrentTimeTool().Execute(context.Background(), `{"unknown":1}`)
			return err
		}},
		{name: "calculator", run: func() error {
			_, err := NewCalculatorTool().Execute(context.Background(), `{"expression":"1+1","unknown":1}`)
			return err
		}},
		{name: "http_request", run: func() error {
			_, err := NewHTTPRequestTool(platformhttp.New()).Execute(context.Background(), `{"url":"https://example.com","unknown":1}`)
			return err
		}},
		{name: "todo_read", run: func() error {
			_, err := NewTodoReadTool(nil).Execute(context.Background(), `{"unknown":1}`)
			return err
		}},
		{name: "todo_write", run: func() error {
			_, err := NewTodoWriteTool(nil).Execute(context.Background(), `{"steps":[],"unknown":1}`)
			return err
		}},
		{name: "todo_update_step", run: func() error {
			_, err := NewTodoUpdateStepTool(nil).Execute(context.Background(), `{"id":"1","status":"completed","unknown":1}`)
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("未知字段应被严格拒绝")
			}
		})
	}
}
