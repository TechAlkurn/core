package middleware

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/components/authorization"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/db/activerecord"
	"github.com/TechAlkurn/core/lib"
	"github.com/TechAlkurn/core/reqctx"
	"github.com/gin-gonic/gin"
)

// verifyToken validates the Authorization header and returns the caller.
// A var so tests can stub it without a database.
var verifyToken = func(token string) (reqctx.Identity, error) {
	user, err := authorization.NewJwtService().VerifyToken(token)
	if err != nil {
		return reqctx.Identity{}, err
	}
	return reqctx.Identity{UserID: user["id"].(uint32), Username: user["username"].(string), Email: user["email"].(string)}, nil
}

// RequestContext seeds every request's context with its language, so routes
// without an auth middleware still forward it to gRPC. Install it on both
// engines before any auth middleware.
func RequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := c.Request.Header.Get("Accept-Language")
		if lg := c.Query("lg"); lib.NotEmpty(lg) {
			lang = lg
		}
		if lib.IsEmpty(lang) {
			lang = config.GetString("primary_language")
		}
		setIdentity(c, reqctx.Identity{Lang: lang})
		c.Next()
	}
}

// setIdentity stores id on the request context; controllers pass that context
// to gRPC, whose client interceptor forwards it (see config.withGrpcAuth).
func setIdentity(c *gin.Context, id reqctx.Identity) {
	c.Request = c.Request.WithContext(reqctx.With(c.Request.Context(), id))
}

// authenticate clears any identity, then verifies the token. It returns
// false (having aborted the request) when the token or session is invalid.
func authenticate(c *gin.Context, token string) bool {
	r := action.NewResponse(c)
	current, _ := reqctx.From(c.Request.Context())
	setIdentity(c, reqctx.Identity{Lang: current.Lang})
	identity, err := verifyToken(token)
	if err != nil {
		r.Abort(err) // Abort the request with an error
		return false
	}
	if identity.UserID == 0 {
		r.Abort(errors.New(cons.LoginSessionNotValid)) // Abort the request with an error
		return false
	}
	identity.Lang = current.Lang
	setIdentity(c, identity)
	return true
}

// publicPages returns a fresh per-request list of pages reachable without a
// token. It must never alias the middleware's captured slice (appending to that
// would leak entries across requests) and must never include request input —
// otherwise a caller could whitelist any page for themselves.
func publicPages(pgs []string, extra ...string) []string {
	pages := make([]string, 0, len(pgs)+len(extra))
	pages = append(pages, pgs...)
	pages = append(pages, extra...)
	if permitted := config.GetString("permitted_pages"); !lib.IsEmpty(permitted) {
		pages = append(pages, strings.Split(permitted, ",")...)
	}
	return pages
}

// JWTAuthMiddleware is a middleware function for AWS JWT authentication
func BackendMiddleware(pgs ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		r := action.NewResponse(c)
		pg := c.Query("key") // Get the key query parameter
		pgs := publicPages(pgs, "config")
		// Append extra pages to the pages slice
		if lib.IsEmpty(pg) { // If the key query parameter is IsEmpty
			u := strings.Split(c.Request.URL.Path, "/") // Split the URL path by "/"
			pg = u[len(u)-1]                            // Get the last element of the split URL path
		}
		token := c.Request.Header.Get("Authorization") // Get the Authorization header
		if lib.IsEmpty(token) {                        // If the Authorization header is IsEmpty
			if pgs != nil && lib.InArray(pg, pgs) { // If the pages slice is not nil and the key query parameter is in the pages slice
				c.Next() // Call the next handler
				return
			}
			r.Abort(errors.New(cons.AuthorizationNotValid)) // Abort the request with an error
			return
		}
		if !authenticate(c, token) {
			return
		}
		// Call the next handler
		c.Next()
	}
}

// JWTAuthMiddleware is a middleware function for AWS JWT authentication
func FrontendMiddleware(pgs ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		r := action.NewResponse(c)
		pg := c.Query("key") // Get the key query parameter
		pgs := publicPages(pgs, "categories", "tag-types", "tags")

		// Append extra pages to the pages slice
		if lib.IsEmpty(pg) { // If the key query parameter is IsEmpty
			u := strings.Split(c.Request.URL.Path, "/") // Split the URL path by "/"
			pg = u[len(u)-1]                            // Get the last element of the split URL path
		}
		token := c.Request.Header.Get("Authorization") // Get the Authorization header
		if lib.IsEmpty(token) {
			if pgs != nil && lib.InArray(pg, pgs) { // If the pages slice is not nil and the key query parameter is in the pages slice
				c.Next() // Call the next handler
				return
			}
			r.Abort(errors.New(cons.AuthorizationNotValid)) // Abort the request with an error
			return
		}
		if !authenticate(c, token) {
			return
		}
		/*
			if !IsActiveUser(userID) {
				r.Abort(errs.New("Your account has been deactivated, to reactivate it, please contact the admin."))
			}
		*/
		// Call the next handler
		c.Next()
	}
}

func LogIsEmptyResponse() gin.HandlerFunc {
	return func(c *gin.Context) {
		bw := &bodyWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = bw
		c.Next()
		if bw.body.Len() == 0 && c.Writer.Status() == http.StatusOK {
			config.Log.Warnf("IsEmpty response for %s", c.Request.URL.String())
		}
	}
}

func ResponseLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		writer := &bodyWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()

		if c.Writer.Status() == http.StatusOK && writer.body.Len() == 0 {
			config.Log.Warnf("Blank response for path: %s", c.Request.URL.Path)
		}
	}
}

type bodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func IsActiveUser(userID uint32) bool {
	var user map[string]any
	db := activerecord.Raw("SELECT * from `user` WHERE `id` = ? AND `status` = ?", userID, cons.USER_STATUS_ACTIVE).Scan(&user)
	if db.Error != nil {
		return false
	}
	return user != nil
}
