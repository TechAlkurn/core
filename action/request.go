package action

import (
	"net/http"
	"net/url"
)

type Context struct {
	Uri     string
	Request *http.Request
}

type IRequest interface {
	Query(key string) string
	IgnoreNotification() bool
}

func NewRequest(uri string) IRequest {
	req, _ := http.NewRequest("GET", "", nil)
	parsedURL, _ := url.ParseQuery(uri)
	req.URL.RawQuery = parsedURL.Encode()
	return &Context{Uri: uri, Request: req}
}

func (r *Context) PgRequest() *http.Request {
	req, _ := http.NewRequest("GET", "", nil)
	parsedURL, _ := url.ParseQuery(r.Uri)
	req.URL.RawQuery = parsedURL.Encode()
	r.Request = req
	return req
}

func (r *Context) Query(key string) string {
	return r.Request.URL.Query().Get(key)
}

func (r *Context) IgnoreNotification() bool {
	is_ignore := r.Query("is_ignore")
	return is_ignore == "1" || is_ignore == "yes" || is_ignore == "YES" || is_ignore == "Yes"
}

func Request(query string) *Context {
	req := Context{Uri: query}
	req.PgRequest()
	return &req
}
