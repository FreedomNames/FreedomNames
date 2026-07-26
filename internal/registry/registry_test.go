package registry

import "testing"

func TestIsBareName(t *testing.T) {
	cases := map[string]bool{
		"mysite.fn": true, // bare
		"mysite.mugh925ipvygve5a4p0p8ai5vp4o2dofmeeok84hamb238j2r9o3.fn": false, // self-certifying
		"example.com": false, // not .fn
		// A long human label must NOT be mistaken for a pubkey id: it does not
		// decode as a base36 sha2-256 multihash, so the name is bare.
		"blog.my-very-long-organization-department-name-somewhere.fn": true,
		// A random 52-char base36-ish string that is not a valid multihash is
		// still bare, regardless of its length.
		"blog.zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz.fn": true,
	}
	for name, want := range cases {
		if got := IsBareName(name); got != want {
			t.Errorf("IsBareName(%q) = %v, want %v", name, got, want)
		}
	}
}
