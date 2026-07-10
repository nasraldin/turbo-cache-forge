package localauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// First-party HS256 JWT. We both sign and verify, so a minimal stdlib
// implementation avoids a JWT dependency while staying standard-shaped.

type claims struct {
	Iss      string `json:"iss"`
	Sub      string `json:"sub"`
	Username string `json:"username"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
}

var errBadToken = errors.New("invalid token")

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func sign(secret []byte, signingInput string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(signingInput))
	return m.Sum(nil)
}

func signToken(secret []byte, c claims) string {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(c)
	signingInput := header + "." + b64(payload)
	return signingInput + "." + b64(sign(secret, signingInput))
}

func verifyToken(secret []byte, tok string, now time.Time) (claims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return claims{}, errBadToken
	}
	signingInput := parts[0] + "." + parts[1]
	want := sign(secret, signingInput)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, got) {
		return claims{}, errBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}, errBadToken
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return claims{}, errBadToken
	}
	if now.Unix() >= c.Exp {
		return claims{}, errBadToken
	}
	return c, nil
}
