package localauth

import (
	"strings"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("s3cret")
	now := time.Unix(1_000_000, 0)
	tok := signToken(secret, claims{Iss: "turbo-cache-forge", Sub: "local:root", Username: "root", Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()})
	c, err := verifyToken(secret, tok, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Username != "root" || c.Sub != "local:root" {
		t.Fatalf("claims = %+v", c)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	secret := []byte("s3cret")
	now := time.Unix(1_000_000, 0)
	tok := signToken(secret, claims{Exp: now.Unix()}) // exp == now -> expired
	if _, err := verifyToken(secret, tok, now); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	tok := signToken([]byte("right"), claims{Exp: now.Add(time.Hour).Unix()})
	if _, err := verifyToken([]byte("wrong"), tok, now); err == nil {
		t.Fatal("expected wrong-secret verify to fail")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if _, err := verifyToken([]byte("s"), "not-a-jwt", time.Now()); err == nil {
		t.Fatal("expected malformed token to fail")
	}
	if _, err := verifyToken([]byte("s"), strings.Repeat("a.", 2), time.Now()); err == nil {
		t.Fatal("expected 2-part token to fail")
	}
}
