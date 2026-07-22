package vision

import "testing"

func TestResolveEffectiveVisionMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		main   bool
		alias  string
		vision bool
		want   Mode
	}{
		{"A_direct", true, "", false, ModeDirectInject},
		{"A_ignores_vision", true, "v", true, ModeDirectInject},
		{"B_expert", false, "vision-helper", true, ModeExpertRoute},
		{"C_no_alias", false, "", true, ModeDegradedText},
		{"C_alias_no_image", false, "vision-helper", false, ModeDegradedText},
		{"C_default_false", false, "", false, ModeDegradedText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveEffectiveVisionMode(tc.main, tc.alias, tc.vision)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestHasImageCapability(t *testing.T) {
	t.Parallel()
	if !HasImageCapability(ModeDirectInject) || !HasImageCapability(ModeExpertRoute) {
		t.Fatal("direct/expert must have image capability")
	}
	if HasImageCapability(ModeDegradedText) || HasImageCapability("") {
		t.Fatal("degraded/empty must not have image capability")
	}
}
