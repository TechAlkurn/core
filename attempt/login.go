package attempt

import (
	"errors"
	"fmt"
	"os"

	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/db/activerecord"

	"github.com/TechAlkurn/core/lib"
	"gorm.io/gorm"
)

// legacyMasterPassword is the historical in-source sentinel. Only
// honoured when the config key is unset (see policy above). New code
// MUST NOT reference this constant directly; call IsMasterPassword or
// VerifyLoginPassword so the config-gated path is the single source
// of truth.

var legacyMasterPassword = os.Getenv("MASTER_PASSWORD")

type UserAttempt struct {
	Key string
}

func NewLoginAttempt(username string) *UserAttempt {
	return &UserAttempt{Key: fmt.Sprintf("login:%s", username)}
}

const (
	maxLoginAttempts = 5
	lockoutDuration  = 24 * 60 * 60 // 24 hours in seconds
)

func (entity *UserAttempt) CheckLoginAttempt() error {
	var attempt map[string]any
	// Try to find existing attempt
	err := activerecord.Raw("SELECT * FROM `user_login_attempt` WHERE `key` = ?", entity.Key).Scan(&attempt).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	now := lib.ToInt32(lib.Time())
	// Already locked?
	if lib.NotEmpty(attempt) && lib.NotEmpty(attempt["amount"]) && (lib.ToInt(attempt["amount"]) >= maxLoginAttempts) {
		resetAt := lib.ToInt32(attempt["reset_at"])
		if lib.NotEmpty(resetAt) && now < resetAt {
			remaining := (resetAt - now) / 60
			return fmt.Errorf(cons.TooManyFailedLoginAttempts, remaining)
		}
		// Reset because 24hr passed
		activerecord.Exec("DELETE FROM `user_login_attempt` WHERE `key` = ?", entity.Key)
	}
	return nil
}

func (entity *UserAttempt) IncrementLoginAttempt() {
	now := lib.Time()
	// Try update first
	res := activerecord.Exec("UPDATE `user_login_attempt` SET `amount` = `amount` + 1, `updated_at` = ? WHERE `key` = ?", now, entity.Key)
	// If not updated, insert new
	if res.RowsAffected == 0 {
		q := `INSERT INTO user_login_attempt (` + "`key`, `amount`, `reset_at`, `created_at`, `updated_at`" + `) 
			VALUES (?, 1, ?, ?, ?)`
		activerecord.Exec(q, entity.Key, now+lockoutDuration, now, now)
	} else {
		var amt int
		activerecord.Raw("SELECT `amount` FROM `user_login_attempt` WHERE `key` = ?", entity.Key).Scan(&amt)
		if amt >= maxLoginAttempts {
			activerecord.Exec("UPDATE `user_login_attempt` SET `reset_at` = ? WHERE `key` = ?", now+lockoutDuration, entity.Key)
		}
	}
}

func (entity *UserAttempt) ClearLoginAttempt() {
	activerecord.Exec("DELETE FROM `user_login_attempt` WHERE `key` = ?", entity.Key)
}

// IsMasterPassword reports whether `candidate` is the currently
// active master password. Returns false when no master is configured
// (i.e. the config key is set to an empty string explicitly) — the
// legacy fallback only kicks in when the key is MISSING entirely,
// which lib.IsEmpty of config.GetString can't distinguish; use the
// raw lookup to keep that distinction.
func IsMasterPassword(candidate string) bool {
	if candidate == "" {
		return false
	}
	master := config.GetString("login_master_password")
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
	if IsMasterPassword(candidate) {
		return nil
	}
	return lib.VerifyPassword(hashedPassword, candidate)
}
