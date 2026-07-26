package bch

import "testing"

func TestNormalizeRegistryName(t *testing.T) {
	ok := map[string]string{
		"mysite":     "mysite",
		"MySite.fn":  "mysite",
		"my-site.fn": "my-site",
		"a1":         "a1",
	}
	for in, want := range ok {
		got, err := NormalizeName(in)
		if err != nil {
			t.Errorf("normalize(%q) unexpected error: %v", in, err)
		} else if got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
	bad := []string{"", "-bad", "bad-", "has space", "under_score", "a.b", "café"}
	for _, in := range bad {
		if _, err := NormalizeName(in); err == nil {
			t.Errorf("normalize(%q) should have failed", in)
		}
	}
}
