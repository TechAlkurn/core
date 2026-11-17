package config

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Log is the application logger. It wraps logrus so the printf-style methods
// render struct / pointer arguments as readable values (see Readable).
var Log = &Logger{Logger: logrus.New()}

const logTimeFormat = "2006-01-02 15:04:05"

// CustomFormatter renders entries as:
//
//	2006-01-02 15:04:05 INFO  >>> file.go:42 message key=value ...
type CustomFormatter struct{}

func (f *CustomFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	b := entry.Buffer
	if b == nil {
		b = &bytes.Buffer{}
	}

	b.WriteString(entry.Time.Format(logTimeFormat))
	fmt.Fprintf(b, " %-5s", strings.ToUpper(entry.Level.String()))
	if file, line, ok := callerOf(entry); ok {
		fmt.Fprintf(b, " >>> %s:%d", filepath.Base(file), line)
	}
	b.WriteByte(' ')
	b.WriteString(strings.TrimRight(entry.Message, "\n"))

	// Append structured fields (WithField / WithError) in a stable order.
	keys := make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := entry.Data[k]
		if err, ok := v.(error); ok {
			v = err.Error()
		}
		fmt.Fprintf(b, " %s=%v", k, v)
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// callerOf returns the first stack frame outside logrus and this package's
// Logger wrapper, i.e. the line that actually issued the log call.
func callerOf(entry *logrus.Entry) (string, int, bool) {
	pcs := make([]uintptr, 32)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	for {
		frame, more := frames.Next()
		fn := frame.Function
		if !strings.HasPrefix(fn, "github.com/sirupsen/logrus.") &&
			!strings.HasPrefix(fn, "app/pkg/core/config.(*Logger).") &&
			!strings.HasPrefix(fn, "app/pkg/core/config.(*CustomFormatter).") {
			return frame.File, frame.Line, true
		}
		if !more {
			break
		}
	}
	if entry.HasCaller() {
		return entry.Caller.File, entry.Caller.Line, true
	}
	return "", 0, false
}

func InitLog() {
	Log.SetFormatter(&CustomFormatter{})
	// Caller is resolved in CustomFormatter (callerOf) so the Logger wrapper
	// frames are skipped; logrus' own caller report would point at log.go.
	Log.SetReportCaller(false)
	Log.SetLevel(logrus.InfoLevel)
	if gin.Mode() == gin.DebugMode {
		Log.SetLevel(logrus.DebugLevel)
	}
}

// LoggerMiddleware logs each HTTP request after it is processed.
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		Log.WithFields(logrus.Fields{
			"status_code": c.Writer.Status(),
			"latency":     time.Since(start),
			"client_ip":   c.ClientIP(),
			"method":      c.Request.Method,
			"path":        c.Request.URL.Path,
		}).Info("Request processed")
	}
}
