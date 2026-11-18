package base

import (
	"reflect"
	"testing"
)

func TestArgs(t *testing.T) {
	in := &Request{Query: "slug=abc&uid=7&primary_id=3&empty="}
	cases := []struct {
		name   string
		params []any
		want   map[string]any
	}{
		{"string", []any{"slug"}, map[string]any{"slug": "abc"}},
		{"renamed", []any{map[string]string{"user_id": "uid"}}, map[string]any{"user_id": "7"}},
		{"pinned", []any{"slug", map[string]any{"status": 1}}, map[string]any{"slug": "abc", "status": 1}},
		{"empty value falls back to primary_id", []any{"empty"}, map[string]any{"id": "3"}},
		{"no params", nil, map[string]any{"id": "3"}},
	}
	for _, tc := range cases {
		if got := Args(in, tc.params...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	if got := Args(nil); len(got) != 0 {
		t.Errorf("nil request: got %v, want empty", got)
	}
}

func TestSplitList(t *testing.T) {
	if got := splitList("title"); !reflect.DeepEqual(got, []string{"title"}) {
		t.Errorf("single field must be kept: got %v", got)
	}
	if got := splitList(" a, ,b "); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %v", got)
	}
	if got := splitList(""); got != nil {
		t.Errorf("empty: got %v", got)
	}
}
