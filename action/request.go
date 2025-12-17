package action

import (
	"fmt"
	"net/http"
	"net/url"
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

// Build *http.Request from raw query string
func (r *Context) PgRequest() *http.Request {
	httpReq, _ := http.NewRequest("GET", "", nil)

	parsedQuery, _ := url.ParseQuery(r.Uri)
	httpReq.URL.RawQuery = parsedQuery.Encode()

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

	r.Request.URL.RawQuery = q.Encode()
	r.Uri = q.Encode()
}

func (r *Context) SetQueryParams(params map[string]any) {
	for key, value := range params {
		r.SetQueryParam(key, value)
	}
}

func (r *Context) DeleteQueryParam(param string) {
	q := r.Request.URL.Query()
	q.Del(param)

	r.Request.URL.RawQuery = q.Encode()
	r.Uri = q.Encode()
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
