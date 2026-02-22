package config

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func (a Account) Identifier() string {
	if strings.TrimSpace(a.Email) != "" {
		return strings.TrimSpace(a.Email)
	}
	if strings.TrimSpace(a.Mobile) != "" {
		return strings.TrimSpace(a.Mobile)
	}
	// Backward compatibility: old configs may contain token-only accounts.
	// Use a stable non-sensitive synthetic id so they can still join the pool.
	token := strings.TrimSpace(a.Token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "token:" + hex.EncodeToString(sum[:8])
}
