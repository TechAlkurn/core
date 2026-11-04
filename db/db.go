// Package db holds the process-wide GORM connection and logger used by the
// db/activerecord helpers. It has no dependency on any host application:
// the host opens the connection (e.g. via db/drivers) and registers it once
// at startup.
//
//	driver := drivers.NewMysql()
//	db.SetConnection(driver.DB())
//	db.SetLogger(myLogger) // optional; defaults to the standard log package
package db

import (
	"log"
	"sync/atomic"

	"gorm.io/gorm"
)

// Logger is the minimal logging surface the db packages need. logrus,
// *log.Logger and most other loggers satisfy it.
type Logger interface {
	Printf(format string, args ...any)
}

type loggerHolder struct{ Logger }

var (
	conn   atomic.Pointer[gorm.DB]
	logger atomic.Pointer[loggerHolder]
)

// SetConnection registers the GORM connection used by activerecord.
// Call once during startup, before any query runs.
func SetConnection(db *gorm.DB) {
	conn.Store(db)
}

// Connection returns the registered GORM connection. It panics if
// SetConnection has not been called, which is a startup-order bug.
func Connection() *gorm.DB {
	db := conn.Load()
	if db == nil {
		panic("core/db: connection not set; call db.SetConnection during startup")
	}
	return db
}

// SetLogger replaces the logger used for DB diagnostics. Passing nil
// restores the default (standard library log package).
func SetLogger(l Logger) {
	if l == nil {
		logger.Store(nil)
		return
	}
	logger.Store(&loggerHolder{l})
}

// Log returns the registered logger, or the standard library logger when
// none has been set.
func Log() Logger {
	if h := logger.Load(); h != nil {
		return h.Logger
	}
	return log.Default()
}
