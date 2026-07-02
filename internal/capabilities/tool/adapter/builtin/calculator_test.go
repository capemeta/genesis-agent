package builtin

import (
	"context"
	"strings"
	"testing"
)

func TestCalculatorToolExecuteExactFinanceMath(t *testing.T) {
	t.Parallel()

	tool := NewCalculatorTool()
	cases := []struct {
		name   string
		params string
		want   string
	}{
		{
			name:   "decimal addition has no float64 error",
			params: `{"expression":"0.1 + 0.2"}`,
			want:   "0.3",
		},
		{
			name:   "percentage expression",
			params: `{"expression":"100 * (1 + 6.5%)"}`,
			want:   "106.5",
		},
		{
			name:   "multiplication before addition",
			params: `{"expression":"2 + 3 * 4"}`,
			want:   "14",
		},
		{
			name:   "parentheses override precedence",
			params: `{"expression":"(2 + 3) * 4"}`,
			want:   "20",
		},
		{
			name:   "same precedence is evaluated left to right",
			params: `{"expression":"20 / 2 * 3"}`,
			want:   "30",
		},
		{
			name:   "power is right associative",
			params: `{"expression":"2^3^2"}`,
			want:   "512",
		}, {
			name:   "default scale rounds non terminating division",
			params: `{"expression":"10 / 3"}`,
			want:   "3.3333333333",
		},
		{
			name:   "custom half up scale",
			params: `{"expression":"10 / 3", "scale":2}`,
			want:   "3.33",
		},

		{
			name:   "explicit scale rounds exact decimal",
			params: `{"expression":"2.345", "scale":2}`,
			want:   "2.35",
		},
		{
			name:   "half even output rounding",
			params: `{"expression":"2.345", "scale":2, "rounding_mode":"half_even"}`,
			want:   "2.34",
		},
		{
			name:   "round function",
			params: `{"expression":"round(2.345, 2)"}`,
			want:   "2.35",
		},
		{
			name:   "sqrt is kept with configured precision",
			params: `{"expression":"sqrt(2)", "scale":6}`,
			want:   "1.414214",
		},
		{
			name:   "perfect square sqrt stays clean",
			params: `{"expression":"sqrt(4)"}`,
			want:   "2",
		},
		{
			name:   "integer power",
			params: `{"expression":"2^10"}`,
			want:   "1024",
		},
		{
			name:   "negative exponent",
			params: `{"expression":"2^-2"}`,
			want:   "0.25",
		},
		{
			name:   "power binds tighter than unary minus",
			params: `{"expression":"-2^2"}`,
			want:   "-4",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tool.Execute(context.Background(), tc.params)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Execute() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCalculatorToolExecuteErrors(t *testing.T) {
	t.Parallel()

	tool := NewCalculatorTool()
	cases := []struct {
		name        string
		params      string
		wantErrPart string
	}{
		{
			name:        "divide by zero",
			params:      `{"expression":"1 / 0"}`,
			wantErrPart: "除数不能为零",
		},
		{
			name:        "sqrt negative is rejected",
			params:      `{"expression":"sqrt(-1)"}`,
			wantErrPart: "sqrt的参数不能为负数",
		},
		{
			name:        "fractional exponent is rejected",
			params:      `{"expression":"4^0.5"}`,
			wantErrPart: "幂指数必须是整数",
		},
		{
			name:        "zero with negative exponent is rejected",
			params:      `{"expression":"0^-1"}`,
			wantErrPart: "零不能作为负指数幂的底数",
		},
		{
			name:        "scale is bounded",
			params:      `{"expression":"1 / 3", "scale":19}`,
			wantErrPart: "scale必须在0到18之间",
		},
		{
			name:        "invalid character",
			params:      `{"expression":"1 + @"}`,
			wantErrPart: "预期数字",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tool.Execute(context.Background(), tc.params)
			if err == nil {
				t.Fatal("Execute() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Fatalf("Execute() error = %q, want containing %q", err.Error(), tc.wantErrPart)
			}
		})
	}
}

func TestCalculatorToolExecuteCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewCalculatorTool().Execute(ctx, `{"expression":"1 + 1"}`)
	if err == nil || !strings.Contains(err.Error(), "计算已取消") {
		t.Fatalf("Execute() error = %v, want canceled error", err)
	}
}
