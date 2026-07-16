package write_file

import (
	"strings"
	"testing"
)

func TestPathSchemaDistinguishesWorkAndFinalOutput(t *testing.T) {
	info := (&Tool{}).GetInfo()
	pathSchema := info.Parameters.Properties["path"]
	if pathSchema == nil {
		t.Fatal("missing path parameter schema")
	}
	for _, want := range []string{"$WORK_DIR", "$OUTPUT_DIR", "最终文本交付物"} {
		if !strings.Contains(pathSchema.Description, want) {
			t.Fatalf("path description %q does not contain %q", pathSchema.Description, want)
		}
	}
}
