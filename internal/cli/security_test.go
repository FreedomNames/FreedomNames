package cli

import (
	"testing"
)

// --- CLI: a label becomes a filename, so it must not escape the key dir ---

func TestCheckLabelRejectsPathTraversal(t *testing.T) {
	bad := []string{
		"", "..", ".", "../../etc/passwd", "a/b", `a\b`, "a..b", "-lead", "sp ace", "nul\x00l",
	}
	for _, label := range bad {
		if err := checkLabel(label); err == nil {
			t.Errorf("checkLabel(%q) = nil, want an error", label)
		}
	}
	good := []string{"mysite", "blog.mysite", "my-site", "my_site", "site123"}
	for _, label := range good {
		if err := checkLabel(label); err != nil {
			t.Errorf("checkLabel(%q) = %v, want nil", label, err)
		}
	}
	// keyPath must refuse to build a path outside the keys directory.
	if _, err := keyPath("../../escape"); err == nil {
		t.Error("keyPath escaped the keys directory")
	}
}

// --- registry history scanning ---
