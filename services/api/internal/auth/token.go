package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

func GenerateToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = "turbo_" + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// ponytail: no constant-time compare needed — we look up the SHA-256 hash of
// a 256-bit random token by indexed equality against api_keys.token_hash;
// there is no low-entropy secret here for a timing attack to extract.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
