package base

import (
	"errors"
	"maps"
	"strings"

	"github.com/TechAlkurn/core/db/activerecord"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/lib"
	"github.com/morkid/paginate"
)

// ErrRecordNotFound is returned by View and Del when the request carries no
// usable filter. Same text as the application's enum.RecordNotFound.
var ErrRecordNotFound = errors.New("Record not found")

// Payload is the request a form or service reads: the URL query string and
// the JSON body. The generated *gen.Request satisfies it, so this package
// stays free of any application/transport import. Implementations must be
// nil-safe (protobuf getters are), and a nil Payload reads as empty.
type Payload interface {
	GetQuery() string
	GetForm() []byte
}

// Request is a plain Payload for callers without a transport request
// (cron jobs, tests).
type Request struct {
	Query string
	Form  []byte
}

func (r *Request) GetQuery() string {
	if r == nil {
		return ""
	}
	return r.Query
}

func (r *Request) GetForm() []byte {
	if r == nil {
		return nil
	}
	return r.Form
}

func queryOf(in Payload) string {
	if in == nil {
		return ""
	}
	return in.GetQuery()
}

func formOf(in Payload) []byte {
	if in == nil {
		return nil
	}
	return in.GetForm()
}

// Search paginates model T through DataQuery-based filtering, applies
// transform to each row, and returns the paginate.Page. Optional params
// overlay query values on the request before the query runs.
//
//	base.Search(in, func(item *model.UserAward) any {
//		return helpers.NewUserAward(item).Transform()
//	})
func Search[T any](in Payload, transform func(*T) any, params ...map[string]any) paginate.Page {
	c := action.Request(queryOf(in))
	for _, p := range params {
		c.SetQueryParams(p)
	}
	query := activerecord.NewAR[T]().DataQuery(c, splitList(c.Query("exclude"))...)
	items := []*T{}
	result := paginate.New().With(query).Request(c.Request).Response(&items)
	pages := make([]any, 0, len(items))
	for _, item := range items {
		pages = append(pages, transform(item))
	}
	result.Items = &pages
	return result
}

// View loads the one T matching Args(in, params...) and returns it transformed.
func View[T any](in Payload, transform func(*T) any, params ...any) (any, error) {
	args := Args(in, params...)
	if len(args) == 0 {
		return nil, ErrRecordNotFound
	}
	item, err := activerecord.NewAR[T]().FindOne(args)
	if err != nil {
		return nil, err
	}
	return transform(item), nil
}

// Del deletes every T matching Args(in, params...). It refuses to run without
// a filter, so a bare request can never wipe the table.
func Del[T any](in Payload, params ...any) (bool, error) {
	args := Args(in, params...)
	if len(args) == 0 {
		return false, ErrRecordNotFound
	}
	if err := activerecord.NewAR[T]().DeleteAll(args); err != nil {
		return false, err
	}
	return true, nil
}

// Args builds a WHERE map from the request query. Each param may be:
//   - string:            use as both query-param name and db column
//   - []string:          multiple string params, same rule
//   - map[string]string: {db_column: query_param_name}
//   - map[string]any:    {db_column: value}, copied verbatim (pinned filters
//     like {"status": 1} that don't come from the request)
//
// Empty query values are skipped. If nothing matches, it falls back to
// id = primary_id.
func Args(in Payload, params ...any) map[string]any {
	args := map[string]any{}
	c := action.Request(queryOf(in))
	put := func(col, key string) {
		if v := c.Query(key); lib.NotEmpty(v) {
			args[col] = v
		}
	}
	for _, param := range params {
		switch v := param.(type) {
		case string:
			put(v, v)
		case []string:
			for _, s := range v {
				put(s, s)
			}
		case map[string]string:
			for col, key := range v {
				put(col, key)
			}
		case map[string]any:
			maps.Copy(args, v)
		}
	}
	if len(args) == 0 {
		if id := c.Query("primary_id"); lib.NotEmpty(id) {
			args["id"] = id
		}
	}
	return args
}

// splitList splits a comma-separated value, trimming spaces and dropping
// empty entries ("a, ,b" → ["a", "b"]).
func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
