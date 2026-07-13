package service

import "testing"

func TestDetectDependencyInstallCommand(t *testing.T) {
	cases := []struct {
		command string
		manager string
		pkg     string
	}{
		{"npm install pptxgenjs", "npm", "pptxgenjs"},
		{"npm i -g pptxgenjs@3.12.0", "npm", "pptxgenjs"},
		{"python -m pip install markitdown[pptx]", "pip", "markitdown"},
		{"sh -lc \"pip install Pillow==10.0.0\"", "pip", "Pillow"},
	}
	for _, tc := range cases {
		got, ok := detectDependencyInstallCommand(tc.command)
		if !ok {
			t.Fatalf("%q not detected", tc.command)
		}
		if got.Manager != tc.manager {
			t.Fatalf("%q manager=%q", tc.command, got.Manager)
		}
		missing := installCommandMissingDeps(got)
		if len(missing) != 1 || missing[0].Name != tc.pkg {
			t.Fatalf("%q missing=%+v", tc.command, missing)
		}
	}
}

func TestDetectDependencyInstallCommandIgnoresNormalCommands(t *testing.T) {
	for _, command := range []string{"npm --version", "npm run build", "node create_deck.js", "python scripts/thumbnail.py deck.pptx"} {
		if got, ok := detectDependencyInstallCommand(command); ok {
			t.Fatalf("%q detected as %+v", command, got)
		}
	}
}
