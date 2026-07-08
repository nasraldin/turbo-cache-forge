package auth

import (
	"strings"
	"testing"
)

func TestGenerateTokenRoundTrips(t *testing.T) {
	tok, hash, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "turbo_") {
		t.Errorf("token %q missing turbo_ prefix", tok)
	}
	if HashToken(tok) != hash {
		t.Error("HashToken(token) != returned hash")
	}
	tok2, _, _ := GenerateToken()
	if tok2 == tok {
		t.Error("tokens must be unique")
	}
}
