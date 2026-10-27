// Package reqctx carries the per-request identity (logged-in user, language)
// and the current gRPC request.
//
// It replaces lib.SetMuStorage / lib.GetMuStorage / lib.LoggedId, which used
// one process-wide map: concurrent requests overwrote each other's user id.
//
// Two ways in:
//
//   - context: With / From. The HTTP middleware puts the verified Identity into
//     c.Request.Context(); the gRPC client interceptor forwards it as metadata.
//   - goroutine-bound: Bind / UserID / Lang / Request. The gRPC server binds the
//     Identity to the handler goroutine for the duration of one RPC, so the many
//     call sites that have no ctx (forms, services, helpers) read the right user.
//
// The goroutine-bound store is a temporary bridge until identity is passed
// explicitly. It does NOT cross `go func()` boundaries — use Go to propagate.
package reqctx

import (
	"bytes"
	"context"
	"runtime"
	"strconv"
	"sync"
)

// Identity is what one request knows about its caller.
type Identity struct {
	UserID   uint32
	Username string
	Email    string
	Lang     string
}

type ctxKey struct{}

// With returns a copy of ctx carrying id.
func With(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns the Identity stored in ctx, if any.
func From(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

type entry struct {
	id  Identity
	req any
}

var store sync.Map // goroutine id (uint64) -> *entry

// Bind attaches id and req to the calling goroutine and returns a release
// func that MUST be deferred: gRPC and net/http reuse goroutines, so a missed
// release would leak this identity into the next request on that goroutine.
func Bind(id Identity, req any) (release func()) {
	g := goid()
	store.Store(g, &entry{id: id, req: req})
	return func() { store.Delete(g) }
}

func current() *entry {
	if e, ok := store.Load(goid()); ok {
		return e.(*entry)
	}
	return nil
}

// Current returns the Identity bound to the calling goroutine (zero if none).
func Current() Identity {
	if e := current(); e != nil {
		return e.id
	}
	return Identity{}
}

// UserID is the logged-in user for the current request, 0 if anonymous.
func UserID() uint32 { return Current().UserID }

// Lang is the request language ("" if the caller sent none).
func Lang() string { return Current().Lang }

// IsOwner reports whether userID is the logged-in user.
func IsOwner(userID uint32) bool { return UserID() == userID }

// Request returns the request bound to the calling goroutine (nil if none).
func Request() any {
	if e := current(); e != nil {
		return e.req
	}
	return nil
}

// SetUserID changes the user for the rest of the current request (e.g. 0
// before a signup, so the new row isn't attributed to a stale session). No-op
// when nothing is bound.
func SetUserID(userID uint32) {
	if e := current(); e != nil {
		store.Store(goid(), &entry{id: Identity{UserID: userID, Lang: e.id.Lang}, req: e.req})
	}
}

// Go runs fn in a new goroutine with the caller's identity and request bound,
// so background work (mail, notifications) still knows the user and language.
func Go(fn func()) {
	e := current()
	go func() {
		if e != nil {
			defer Bind(e.id, e.req)()
		}
		fn()
	}()
}

var goroutinePrefix = []byte("goroutine ")

// goid parses the current goroutine's id from runtime.Stack ("goroutine 123
// [running]:..."). That header format has been stable since Go 1.0.
func goid() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, goroutinePrefix)
	if i := bytes.IndexByte(b, ' '); i > 0 {
		b = b[:i]
	}
	n, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		panic("reqctx: cannot parse goroutine id from " + strconv.Quote(string(buf[:])))
	}
	return n
}
