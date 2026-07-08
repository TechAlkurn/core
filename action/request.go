package actionrequest

import (
	"fmt"
	"net/http"
)

type Context struct {
	Uri     string
	Request *http.Request
}

type IRequest interface {
	Query(key string) string
	SetQueryParam(param string, value any)
	SetQueryParams(params map[string]any)
	DeleteQueryParam(param string)
	HasQueryParam(param string) bool
	ToQueryString() string
	IgnoreNotification() bool
	IsParam(arg string) bool
}

func NewRequest(uri string) IRequest {
	req := &Context{Uri: uri}
	req.PgRequest()
	return req
}

// Factory alternative
func Request(query string) *Context {
	req := &Context{Uri: query}
	req.PgRequest()
	return req
}

// Build *http.Request from raw query string.
// An empty target URL always parses successfully, so httpReq (and its URL) is
// never nil. The raw query is assigned verbatim — parsing then re-encoding would
// reorder the params and silently drop the whole query on a parse error.
func (r *Context) PgRequest() *http.Request {
	httpReq, _ := http.NewRequest(http.MethodGet, "", nil)
	httpReq.URL.RawQuery = r.Uri
	r.Request = httpReq
	return httpReq
}

func (r *Context) Query(key string) string {
	return r.Request.URL.Query().Get(key)
}

func (r *Context) HasQueryParam(key string) bool {
	_, exists := r.Request.URL.Query()[key]
	return exists
}

func (r *Context) SetQueryParam(param string, value any) {
	q := r.Request.URL.Query()
	q.Set(param, toString(value))

	encoded := q.Encode()
	r.Request.URL.RawQuery = encoded
	r.Uri = encoded
}

func (r *Context) SetQueryParams(params map[string]any) {
	for key, value := range params {
		r.SetQueryParam(key, value)
	}
	r.Uri = r.ToQueryString()
}

func (r *Context) DeleteQueryParam(param string) {
	q := r.Request.URL.Query()
	q.Del(param)

	encoded := q.Encode()
	r.Request.URL.RawQuery = encoded
	r.Uri = encoded
}

func (r *Context) ToQueryString() string {
	return r.Request.URL.RawQuery
}

func (r *Context) IgnoreNotification() bool {
	isIgnore := r.Query("is_ignore")
	switch isIgnore {
	case "1", "yes", "YES", "Yes":
		return true
	}
	return false
}

func (r *Context) IsParam(arg string) bool {
	return (r.Query(arg) != "")
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
