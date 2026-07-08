package turbo

import (
	"strings"
	"testing"
)

func TestValidHash(t *testing.T) {
	longHash := strings.Repeat("a", 600)

	cases := []struct {
		name string
		hash string
		want bool
	}{
		{"normal hash", "a1b2c3d4e5f6", true},
		{"empty", "", false},
		{"path traversal", "../team-b/secret", false},
		{"encoded slash literal", "..%2Fx", false},
		{"single slash", "a/b", false},
		{"double dot inside", "a..b", false},
		{"bare dotdot", "..", false},
		{"too long", longHash, false},
		{"backslash", `a\b`, false},
		{"space", "a b", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validHash(tc.hash); got != tc.want {
				t.Errorf("validHash(%q) = %v, want %v", tc.hash, got, tc.want)
			}
		})
	}
}
