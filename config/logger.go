package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/sirupsen/logrus"
)

// Logger wraps *logrus.Logger and converts every argument with Readable before
// formatting, so structs and pointers print their values instead of addresses
// like 0xc00231f1a0. All other logrus methods (WithField, SetLevel, ...) are
// promoted from the embedded logger unchanged.
type Logger struct {
	*logrus.Logger
}

// func (l *Logger) Printf(format string, args ...any) { l.Logger.Printf(format, readableArgs(args)...) }
// func (l *Logger) Debugf(format string, args ...any) { l.Logger.Debugf(format, readableArgs(args)...) }
// func (l *Logger) Infof(format string, args ...any) { l.Logger.Infof(format, readableArgs(args)...) }

func (l *Logger) Warnf(format string, args ...any) { l.Logger.Warnf(format, readableArgs(args)...) }

func (l *Logger) Errorf(format string, args ...any) { l.Logger.Errorf(format, readableArgs(args)...) }

func (l *Logger) Fatalf(format string, args ...any) { l.Logger.Fatalf(format, readableArgs(args)...) }

func (l *Logger) Panicf(format string, args ...any) { l.Logger.Panicf(format, readableArgs(args)...) }

func (l *Logger) Print(args ...any) { l.Logger.Print(readableArgs(args)...) }
func (l *Logger) Debug(args ...any) { l.Logger.Debug(readableArgs(args)...) }

// func (l *Logger) Info(args ...any)  { l.Logger.Info(readableArgs(args)...) }
func (l *Logger) Warn(args ...any)  { l.Logger.Warn(readableArgs(args)...) }
func (l *Logger) Error(args ...any) { l.Logger.Error(readableArgs(args)...) }
func (l *Logger) Fatal(args ...any) { l.Logger.Fatal(readableArgs(args)...) }
func (l *Logger) Panic(args ...any) { l.Logger.Panic(readableArgs(args)...) }

func (l *Logger) Println(args ...any) { l.Logger.Println(readableArgs(args)...) }
func (l *Logger) Debugln(args ...any) { l.Logger.Debugln(readableArgs(args)...) }
func (l *Logger) Infoln(args ...any)  { l.Logger.Infoln(readableArgs(args)...) }
func (l *Logger) Warnln(args ...any)  { l.Logger.Warnln(readableArgs(args)...) }
func (l *Logger) Errorln(args ...any) { l.Logger.Errorln(readableArgs(args)...) }
func (l *Logger) Fatalln(args ...any) { l.Logger.Fatalln(readableArgs(args)...) }
func (l *Logger) Panicln(args ...any) { l.Logger.Panicln(readableArgs(args)...) }

// readableArgs replaces struct / pointer / composite arguments with their
// Readable string. Scalars are passed through untouched so verbs like %d, %.2f
// and %q keep working.
func readableArgs(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		if needsReadable(a) {
			out[i] = Readable(a)
		} else {
			out[i] = a
		}
	}
	return out
}

func needsReadable(a any) bool {
	if a == nil {
		return false
	}
	switch a.(type) {
	case error, fmt.Stringer, []byte:
		return false
	}
	switch reflect.TypeOf(a).Kind() {
	case reflect.Pointer, reflect.Struct, reflect.Map, reflect.Slice, reflect.Array, reflect.Interface:
		return true
	}
	return false
}

// maxReadableDepth bounds recursion so self-referencing structs can't loop.
const maxReadableDepth = 6

// Readable renders v as a human-readable string: pointers are dereferenced,
// nil pointers print as <nil>, structs print as {Field:value ...} (unexported
// and empty embedded fields are skipped), and types with an Error or String
// method use it.
func Readable(v any) string {
	var b strings.Builder
	writeReadable(&b, reflect.ValueOf(v), 0)
	return b.String()
}

func writeReadable(b *strings.Builder, v reflect.Value, depth int) {
	if !v.IsValid() {
		b.WriteString("<nil>")
		return
	}
	if depth > maxReadableDepth {
		b.WriteString("...")
		return
	}

	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if v.IsNil() {
			b.WriteString("<nil>")
			return
		}
	}

	if s, ok := stringMethod(v); ok {
		b.WriteString(s)
		return
	}

	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		writeReadable(b, v.Elem(), depth+1)
	case reflect.Struct:
		t := v.Type()
		b.WriteString(t.Name())
		b.WriteByte('{')
		first := true
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			var fb strings.Builder
			writeReadable(&fb, v.Field(i), depth+1)
			// Skip embedded helpers with nothing to show (e.g. activerecord.ActiveRecord[T]).
			if f.Anonymous && strings.HasSuffix(fb.String(), "{}") {
				continue
			}
			if !first {
				b.WriteByte(' ')
			}
			first = false
			b.WriteString(f.Name)
			b.WriteByte(':')
			b.WriteString(fb.String())
		}
		b.WriteByte('}')
	case reflect.Map:
		b.WriteString("map[")
		keys := v.MapKeys()
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeReadable(b, k, depth+1)
			b.WriteByte(':')
			writeReadable(b, v.MapIndex(k), depth+1)
		}
		b.WriteByte(']')
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			fmt.Fprintf(b, "%s", v.Bytes())
			return
		}
		b.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeReadable(b, v.Index(i), depth+1)
		}
		b.WriteByte(']')
	default:
		if v.CanInterface() {
			fmt.Fprintf(b, "%v", v.Interface())
		} else {
			fmt.Fprintf(b, "%v", v)
		}
	}
}

// stringMethod uses v's Error() or String() method when it has one. A panic
// inside the method (e.g. a nil embedded *time.Time) falls back to reflection.
func stringMethod(v reflect.Value) (s string, ok bool) {
	if !v.CanInterface() {
		return "", false
	}
	defer func() {
		if recover() != nil {
			s, ok = "", false
		}
	}()
	switch x := v.Interface().(type) {
	case error:
		return x.Error(), true
	case fmt.Stringer:
		return x.String(), true
	}
	return "", false
}
