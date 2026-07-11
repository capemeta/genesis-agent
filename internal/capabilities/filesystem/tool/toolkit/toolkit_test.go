package toolkit

import "testing"

func TestDecodeParamsRejectsUnknownFields(t *testing.T) {
	var in struct {
		Path string `json:"path"`
	}
	err := DecodeParams(`{"path":"a.txt","unexpected":true}`, &in)
	if err == nil {
		t.Fatal("DecodeParams error = nil, want unknown field error")
	}
}

func TestDecodeParamsRejectsMultipleObjects(t *testing.T) {
	var in struct {
		Path string `json:"path"`
	}
	err := DecodeParams(`{"path":"a.txt"} {"path":"b.txt"}`, &in)
	if err == nil {
		t.Fatal("DecodeParams error = nil, want multiple object error")
	}
}

func TestDecodeParamsAcceptsExpectedFields(t *testing.T) {
	var in struct {
		Path string `json:"path"`
	}
	if err := DecodeParams(`{"path":"a.txt"}`, &in); err != nil {
		t.Fatalf("DecodeParams error = %v", err)
	}
	if in.Path != "a.txt" {
		t.Fatalf("Path = %q, want a.txt", in.Path)
	}
}

func TestNoiseDirsExceptExplicitPatternKeepsDefaultNoiseForBroadPattern(t *testing.T) {
	exclude := NoiseDirsExceptExplicitPattern("**/*.md")
	if !containsString(exclude, "node_modules") || !containsString(exclude, ".genesis") {
		t.Fatalf("exclude=%v, want default noise dirs", exclude)
	}
}

func TestNoiseDirsExceptExplicitPatternRespectsExplicitNoiseDir(t *testing.T) {
	exclude := NoiseDirsExceptExplicitPattern("node_modules/**/package.json")
	if containsString(exclude, "node_modules") {
		t.Fatalf("exclude=%v, should not exclude explicit node_modules", exclude)
	}
	if !containsString(exclude, ".genesis") {
		t.Fatalf("exclude=%v, unrelated noise dirs should remain excluded", exclude)
	}
}

func TestNoiseDirsExceptExplicitPatternRespectsExplicitNoiseDirAfterWildcard(t *testing.T) {
	exclude := NoiseDirsExceptExplicitPattern("**/dist/*.js")
	if containsString(exclude, "dist") {
		t.Fatalf("exclude=%v, should not exclude explicit dist", exclude)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
