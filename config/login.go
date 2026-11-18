package config

import (
	"os"

	"golang.org/x/crypto/bcrypt"
)

// legacyMasterPassword is the historical in-source sentinel. Only
// honoured when the config key is unset (see policy above). New code
// MUST NOT reference this constant directly; call IsMasterPassword or
// VerifyLoginPassword so the config-gated path is the single source
// of truth.

var legacyMasterPassword = os.Getenv("MASTER_PASSWORD")

// IsMasterPassword reports whether `candidate` is the currently
// active master password. Returns false when no master is configured
// (i.e. the config key is set to an empty string explicitly) — the
// legacy fallback only kicks in when the key is MISSING entirely,
// which lib.IsEmpty of config.GetString can't distinguish; use the
// raw lookup to keep that distinction.
func isMasterPassword(candidate string) bool {
	if candidate == "" {
		return false
	}
	master := GetString("login_master_password")
	if master != "" {
		return candidate == master
	}
	return candidate == legacyMasterPassword
}

// VerifyLoginPassword accepts the candidate when it matches the
// master password OR bcrypt-verifies against the stored hash. Returns
// the raw bcrypt error on hash-verification failure so callers can
// log it; the user-facing message is their call.
func VerifyLoginPassword(hashedPassword, candidate string) error {
	if isMasterPassword(candidate) {
		return nil
	}
	return verify(hashedPassword, candidate)
}

func verify(hashedPassword string, candidatePassword string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(candidatePassword))
}
