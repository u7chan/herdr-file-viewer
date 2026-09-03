package herdr

import (
	"os"
	"testing"
)

func TestHelpContextReadsTheCallerContextEnvironment(t *testing.T) {
	t.Setenv(HelpContextEnv, "tree")
	if got := HelpContext(); got != "tree" {
		t.Fatalf("HelpContext(env=tree) = %q, want tree", got)
	}
	t.Setenv(HelpContextEnv, "preview")
	if got := HelpContext(); got != "preview" {
		t.Fatalf("HelpContext(env=preview) = %q, want preview", got)
	}
}

func TestHelpContextFallsBackToTreeForMissingOrUnknownValues(t *testing.T) {
	for _, value := range []string{"", "bogus", "Tree"} {
		t.Setenv(HelpContextEnv, value)
		if got := HelpContext(); got != "tree" {
			t.Fatalf("HelpContext(env=%q) = %q, want tree fallback", value, got)
		}
	}
	if err := os.Unsetenv(HelpContextEnv); err != nil {
		t.Fatalf("Unsetenv(%s): %v", HelpContextEnv, err)
	}
	if got := HelpContext(); got != "tree" {
		t.Fatalf("HelpContext(unset) = %q, want tree fallback", got)
	}
}

func TestIsHelpEntrypointMatchesTheEntrypointEnvironment(t *testing.T) {
	t.Setenv(EntrypointIDEnv, "help")
	if !IsHelpEntrypoint() {
		t.Fatal("IsHelpEntrypoint(env=help) = false, want true")
	}
	if IsPreviewEntrypoint() {
		t.Fatal("IsPreviewEntrypoint(env=help) = true, want false")
	}
	t.Setenv(EntrypointIDEnv, "files")
	if IsHelpEntrypoint() {
		t.Fatal("IsHelpEntrypoint(env=files) = true, want false")
	}
	if IsPreviewEntrypoint() {
		t.Fatal("IsPreviewEntrypoint(env=files) = true, want false")
	}
	t.Setenv(EntrypointIDEnv, "preview")
	if !IsPreviewEntrypoint() || IsHelpEntrypoint() {
		t.Fatalf("entrypoint dispatch for preview = preview %v help %v, want only preview", IsPreviewEntrypoint(), IsHelpEntrypoint())
	}
}