package action

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
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
	return Request(uri)
}

// Payload is anything that carries a raw query string, e.g. the host's
// generated *gen.Request. It is defined here rather than reused from
// db/activerecord because activerecord imports action.
type Payload interface {
	GetQuery() string
}

// HttpRequest builds a Context from in's query string. A nil in (or a typed
// nil such as a nil *gen.Request from cron) yields an empty query.
//
//	c := action.HttpRequest(in) // in *gen.Request
func HttpRequest(in Payload, params ...map[string]any) *Context {
	query := ""
	if !isNil(in) {
		query = in.GetQuery()
	}

	c := Request(query)
	for _, p := range params {
		c.SetQueryParams(p)
	}
	return c
}

// Request builds a Context from a raw query string.
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

// url returns the request URL, building the request from Uri first when the
// Context was created as a literal (e.g. &action.Context{Uri: q}).
func (r *Context) url() *url.URL {
	if r.Request == nil || r.Request.URL == nil {
		r.PgRequest()
	}
	return r.Request.URL
}

// setRawQuery keeps Uri and the request's RawQuery in sync.
func (r *Context) setRawQuery(raw string) {
	r.url().RawQuery = raw
	r.Uri = raw
}

func (r *Context) Query(key string) string {
	return r.url().Query().Get(key)
}

func (r *Context) HasQueryParam(key string) bool {
	_, exists := r.url().Query()[key]
	return exists
}

// SetQueryParam replaces every value of param with value. The first occurrence
// keeps its position, and a new param goes at the end. Other params are left
// exactly as they were, so their order and encoding are kept.
func (r *Context) SetQueryParam(param string, value any) {
	pair := url.QueryEscape(param) + "=" + url.QueryEscape(toString(value))

	var out []string
	replaced := false
	for _, seg := range splitQuery(r.url().RawQuery) {
		if segmentKey(seg) != param {
			out = append(out, seg)
			continue
		}
		if !replaced {
			out = append(out, pair)
			replaced = true
		}
	}
	if !replaced {
		out = append(out, pair)
	}
	r.setRawQuery(strings.Join(out, "&"))
}

// SetQueryParams sets each param. New params are appended in key order so the
// result does not depend on Go's random map iteration order.
func (r *Context) SetQueryParams(params map[string]any) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		r.SetQueryParam(k, params[k])
	}
}

// DeleteQueryParam removes every value of param and leaves the rest as is.
func (r *Context) DeleteQueryParam(param string) {
	var out []string
	for _, seg := range splitQuery(r.url().RawQuery) {
		if segmentKey(seg) != param {
			out = append(out, seg)
		}
	}
	r.setRawQuery(strings.Join(out, "&"))
}

func (r *Context) ToQueryString() string {
	return r.url().RawQuery
}

func (r *Context) IgnoreNotification() bool {
	switch strings.ToLower(r.Query("is_ignore")) {
	case "1", "yes", "true":
		return true
	}
	return false
}

func (r *Context) IsParam(arg string) bool {
	return r.Query(arg) != ""
}

// splitQuery splits a raw query on "&" and drops empty segments, matching
// url.ParseQuery (which no longer treats ";" as a separator).
func splitQuery(raw string) []string {
	var segs []string
	for s := range strings.SplitSeq(raw, "&") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// segmentKey returns the decoded key of a "key=value" segment. A key that
// cannot be decoded is compared in its raw form.
func segmentKey(seg string) string {
	k, _, _ := strings.Cut(seg, "=")
	if dk, err := url.QueryUnescape(k); err == nil {
		return dk
	}
	return k
}

// isNil reports whether p is nil or a typed nil pointer inside the interface.
func isNil(p Payload) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}

func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
