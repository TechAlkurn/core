package config

import (
	"sync"

	"github.com/TechAlkurn/core/db"
	"github.com/TechAlkurn/core/db/drivers"

	"gorm.io/gorm"
)

var (
	DB        *gorm.DB
	mysqlOnce sync.Once
)

// InitDB opens the MySQL pool and registers it (plus the app logger) with
// github.com/TechAlkurn/core/db so activerecord uses the same connection.
func InitDB() {
	mysqlOnce.Do(func() {
		driver := drivers.NewMysql()
		// NewMysql already opened a pool; reuse it rather than calling
		// Connection() again, which would open a second one.
		DB = driver.DB()
		db.SetConnection(DB)
		db.SetLogger(Log)
	})
}
