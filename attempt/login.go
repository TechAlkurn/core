package attempt

import (
	"errors"
	"fmt"

	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/db/activerecord"

	"github.com/TechAlkurn/core/lib"
	"gorm.io/gorm"
)

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
