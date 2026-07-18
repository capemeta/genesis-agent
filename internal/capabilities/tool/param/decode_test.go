package param

import "testing"

func TestDecodeStrictContract(t *testing.T) {
	var out struct {
		Name string `json:"name"`
	}
	if err := Decode(`{"name":"ok"}`, &out); err != nil || out.Name != "ok" {
		t.Fatalf("合法参数解析失败: out=%+v err=%v", out, err)
	}
	for _, raw := range []string{`{"name":"ok","unknown":1}`, `{"name":"ok"} {}`, `null`, `[]`, ``} {
		if err := Decode(raw, &out); err == nil {
			t.Fatalf("非法参数应失败: %q", raw)
		}
	}
}

func TestDecodeOptionalAcceptsEmpty(t *testing.T) {
	var out struct {
		Query string `json:"query"`
	}
	if err := DecodeOptional("", &out); err != nil {
		t.Fatalf("可选空参数不应失败: %v", err)
	}
}
