package middleware

// rbac.middleware.go — gate an admin route group with the RBAC
// engine ported from yii\rbac\DbManager. Chained AFTER
// BackendMiddleware / FrontendMiddleware so that the request context identity is
// already populated by the time CheckAccess runs.
//
// Usage (backend controller):
//
//     gr.Use(middleware.BackendMiddleware())
//     gr.Use(middleware.RbacMiddleware(
//         middleware.RbacOptions{
//             Prefix:     "core",
//             Exceptions: []string{"config", "translate"},
//         },
//     ))
//
// A request that hits /core?key=auth-item with POST is checked
// against the following permission names, in order — the first hit
// wins:
//
//     1. core:auth-item:post      (prefix + key + method — most specific)
//     2. core:auth-item           (prefix + key)
//     3. auth-item:post           (key + method — no prefix)
//     4. auth-item                (key alone — the loosest)
//
// Admins list whichever names they want to gate in auth_item / edges
// via /core?key=auth-item; the middleware doesn't invent items and
// won't mint permissions on the fly.

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/TechAlkurn/core/components/rbac"
	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/reqctx"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/lib"

	"github.com/gin-gonic/gin"
)

// RbacOptions configures one call-site's behaviour without needing
// per-route middleware definitions.
type RbacOptions struct {
	// Prefix is prepended to every candidate permission name — set
	// to the route group's own tag ("core", "user", "catalog") so
	// permission names stay unambiguous across groups.
	Prefix string

	// Exceptions is the set of ?key= values that bypass the check.
	// Match BackendMiddleware's `pgs` semantics: passthrough for
	// endpoints that are already public or authorised elsewhere.
	Exceptions []string

	// AllowIfAdmin skips the CheckAccess call when the caller's
	// user id equals cons.ADMIN_ID. Default true. Turn off for the
	// rare case where you want admins to be subject to the same
	// permission rows as everyone else.
	AllowIfAdmin bool

	// AllowIfNoAssignments returns true from CheckAccess for users
	// with no auth_assignment rows at all. Default false — Yii2's
	// hasNoAssignments returns true so the manager short-circuits,
	// but we treat that as a hard deny at the middleware layer.

	// DenyMessage overrides the abort message. Defaults to
	// cons.NoPermission.
	DenyMessage string
}

// RbacMiddleware returns a gin handler that runs CheckAccess against
// the derived permission name(s) and aborts with 403 when nothing
// matches. Accepts either variadic RbacOptions (uses the first) or
// nothing (uses defaults).
func RbacMiddleware(opts ...RbacOptions) gin.HandlerFunc {
	o := RbacOptions{AllowIfAdmin: true}
	if len(opts) > 0 {
		o = opts[0]
		// Zero-value bool defaults to false when the caller passes
		// a struct — restore the intended default explicitly.
		if !opts[0].AllowIfAdmin && opts[0].DenyMessage == "" && len(opts[0].Exceptions) == 0 && opts[0].Prefix == "" {
			o.AllowIfAdmin = true
		}
	}
	deny := o.DenyMessage
	if deny == "" {
		deny = cons.NoPermission
	}

	return func(c *gin.Context) {
		key := deriveKey(c)
		if key != "" && lib.InArray(key, o.Exceptions) {
			c.Next()
			return
		}
		id, _ := reqctx.From(c.Request.Context())
		userId := id.UserID
		if userId == 0 {
			action.NewResponse(c).Abort(errors.New(cons.AuthorizationNotValid))
			return
		}
		if o.AllowIfAdmin && userId == cons.ADMIN_ID {
			c.Next()
			return
		}
		method := strings.ToLower(c.Request.Method)
		params := map[string]any{
			"user_id": userId,
			"method":  method,
			"path":    c.Request.URL.Path,
			"key":     key,
		}
		for _, name := range candidateNames(o.Prefix, key, method) {
			if name == "" {
				continue
			}
			if rbac.Rbac().CheckAccess(userId, name, params) {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": deny})
	}
}

// deriveKey matches BackendMiddleware's own key resolution:
// ?key=<pg> when set, otherwise the last URL segment (so /core/foo
// still has "foo" to gate on when the caller omits ?key).
func deriveKey(c *gin.Context) string {
	if k := c.Query("key"); k != "" {
		return k
	}
	segs := strings.Split(strings.Trim(c.Request.URL.Path, "/"), "/")
	if len(segs) == 0 {
		return ""
	}
	return segs[len(segs)-1]
}

// candidateNames returns the permission names to try, most-specific
// first. Empty prefix / key are dropped so a caller that omits one
// still gets sensible checks against the other.
func candidateNames(prefix, key, method string) []string {
	var out []string
	switch {
	case prefix != "" && key != "":
		out = append(out, prefix+":"+key+":"+method, prefix+":"+key)
	case prefix != "" && key == "":
		out = append(out, prefix+":"+method, prefix)
	}
	if key != "" {
		out = append(out, key+":"+method, key)
	}
	return out
}

// RequireLogin runs FrontendMiddleware's token check for the selected
// ?key= values and lets every other request straight through. params picks
// the keys:
//
//	RequireLogin(nil)                                       // every key needs login
//	RequireLogin(map[string]any{"skip": []string{"list"}})  // all keys except "list"
//	RequireLogin(map[string]any{"required": []string{"save", "delete"}})
//	                                                        // only "save" and "delete"
//	RequireLogin(map[string]any{"required": []string{"save", "list"}, "skip": []string{"list"}})
//	                                                        // "save" only — skip wins
//
// With "required" set, only those keys are checked; without it, every key
// is. Keys in "skip" are never checked. The lists may be []string or []any
// (as decoded from JSON).
/*
┌──────────────────────────────────────────┬────────────────────────────────────────────────────┐
│                  params                  │          Which ?key= values need a login           │
├──────────────────────────────────────────┼────────────────────────────────────────────────────┤
│ nil                                      │ every key                                          │
├──────────────────────────────────────────┼────────────────────────────────────────────────────┤
│ {"skip": []string{"list"}}               │ every key except list                              │
├──────────────────────────────────────────┼────────────────────────────────────────────────────┤
│ {"required": []string{"save", "delete"}} │ only save and delete                               │
├──────────────────────────────────────────┼────────────────────────────────────────────────────┤
│ both set                                 │ the required keys, minus any that are also in skip │
└──────────────────────────────────────────┴────────────────────────────────────────────────────┘
*/
func RequireLogin(params map[string]any) gin.HandlerFunc {
	auth := FrontendMiddleware()
	required, hasRequired := stringList(params, "required")
	skip, _ := stringList(params, "skip")
	return func(c *gin.Context) {
		key := c.Query("key")
		if slices.Contains(skip, key) || (hasRequired && !slices.Contains(required, key)) {
			c.Next()
			return
		}
		auth(c) // aborts without a valid token, otherwise calls c.Next()
	}
}

// stringList reads params[name] as a list of strings. ok reports whether
// the entry was present at all, so an explicit empty "required" list still
// means "no key requires login".
func stringList(params map[string]any, name string) (list []string, ok bool) {
	v, ok := params[name]
	if !ok || v == nil {
		return nil, ok && v != nil
	}
	switch l := v.(type) {
	case []string:
		return l, true
	case []any:
		out := make([]string, 0, len(l))
		for _, s := range l {
			out = append(out, lib.ToString(s))
		}
		return out, true
	case string:
		return []string{l}, true
	default:
		panic("middleware.RequireLogin: params[\"" + name + "\"] must be []string")
	}
}
