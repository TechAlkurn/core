package activerecord

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

type jsonValuer map[string]any

func (j jsonValuer) Value() (driver.Value, error) {
	b, err := json.Marshal(j)
	return string(b), err
}

type formSingle struct {
	Id    uint32 `gorm:"primaryKey"`
	Title string
	Meta  string
	Raw   []byte
	Tags  []string `gorm:"serializer:json"`
	Attrs jsonValuer
	Count int
}

type formComposite struct {
	UserId   uint32 `gorm:"primaryKey"`
	ItemName string `gorm:"primaryKey"`
	Note     string
}

func mustSchema(t *testing.T, m any) *schema.Schema {
	t.Helper()
	s, err := schema.Parse(m, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPrimaryKeyFromRequest(t *testing.T) {
	single := mustSchema(t, &formSingle{})
	comp := mustSchema(t, &formComposite{})

	cases := []struct {
		name    string
		sch     *schema.Schema
		query   string
		form    string
		want    map[string]string
		wantErr string
	}{
		{name: "scalar query", sch: single, query: "primary_id=42", want: map[string]string{"id": "42"}},
		{name: "scalar form", sch: single, form: `{"primary_id": 9007199254740993}`, want: map[string]string{"id": "9007199254740993"}},
		{name: "column param", sch: single, query: "id=7", want: map[string]string{"id": "7"}},
		{name: "missing", sch: single, wantErr: "primary_id is required"},
		{
			name: "composite object form", sch: comp, form: `{"primary_id": {"user_id": 1, "ItemName": "admin"}}`,
			want: map[string]string{"user_id": "1", "item_name": "admin"},
		},
		{
			name: "composite json query", sch: comp, query: `primary_id=%7B%22user_id%22%3A1%2C%22item_name%22%3A%22a%22%7D`,
			want: map[string]string{"user_id": "1", "item_name": "a"},
		},
		{name: "composite column params", sch: comp, query: "user_id=1&item_name=x", want: map[string]string{"user_id": "1", "item_name": "x"}},
		{name: "composite scalar", sch: comp, query: "primary_id=1", wantErr: "composite key"},
		{name: "composite partial", sch: comp, form: `{"primary_id": {"user_id": 1}}`, wantErr: `missing key column "item_name"`},
		{name: "non-pk key", sch: comp, form: `{"primary_id": {"user_id": 1, "note": "x"}}`, wantErr: "not a primary key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := map[string]any{}
			if tc.form != "" {
				var err error
				if form, err = decodeForm([]byte(tc.form)); err != nil {
					t.Fatal(err)
				}
			}
			pk, err := primaryKeyFromRequest(tc.sch, tc.query, form)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(pk) != len(tc.want) {
				t.Fatalf("pk = %v, want %v", pk, tc.want)
			}
			for k, v := range tc.want {
				if got := strings.TrimSpace(toStr(pk[k])); got != v {
					t.Fatalf("pk[%s] = %v, want %s", k, pk[k], v)
				}
			}
		})
	}
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestColumnValue(t *testing.T) {
	sch := mustSchema(t, &formSingle{})
	obj := map[string]any{"a": json.Number("1")}
	arr := []any{"x", "y"}

	if v, _ := columnValue(sch.LookUpField("title"), "plain"); v != "plain" {
		t.Fatalf("scalar changed: %v", v)
	}
	if v, err := columnValue(sch.LookUpField("meta"), obj); err != nil || v != `{"a":1}` {
		t.Fatalf("string col: %v %v", v, err)
	}
	if v, err := columnValue(sch.LookUpField("raw"), arr); err != nil || string(v.([]byte)) != `["x","y"]` {
		t.Fatalf("[]byte col: %v %v", v, err)
	}
	if v, err := columnValue(sch.LookUpField("tags"), arr); err != nil || v != `["x","y"]` {
		t.Fatalf("serializer col: %v %v", v, err)
	}
	if _, err := columnValue(sch.LookUpField("tags"), obj); err == nil {
		t.Fatal("expected shape mismatch for []string <- object")
	}
	v, err := columnValue(sch.LookUpField("attrs"), obj)
	if err != nil {
		t.Fatal(err)
	}
	if dv, _ := v.(driver.Valuer).Value(); dv != `{"a":1}` {
		t.Fatalf("valuer col: %v", dv)
	}
	if _, err := columnValue(sch.LookUpField("count"), obj); err == nil {
		t.Fatal("expected error for int <- object")
	}
}

func TestWhereFromValue(t *testing.T) {
	single := mustSchema(t, &formSingle{})
	comp := mustSchema(t, &formComposite{})

	cases := []struct {
		name    string
		sch     *schema.Schema
		in      any
		want    map[string]any
		wantErr string
	}{
		{name: "scalar", sch: single, in: 42, want: map[string]any{"id": 42}},
		{
			name: "primary_id alias plus column", sch: single, in: map[string]any{"primary_id": 7, "title": "123456"},
			want: map[string]any{"id": 7, "title": "123456"},
		},
		{name: "go field name", sch: single, in: map[string]any{"Id": 7, "Meta": nil}, want: map[string]any{"id": 7, "meta": nil}},
		{name: "composite", sch: comp, in: map[string]any{"user_id": 1, "item_name": "a"}, want: map[string]any{"user_id": 1, "item_name": "a"}},
		{name: "missing", sch: single, wantErr: "primary_id is required"},
		{name: "no key column", sch: single, in: map[string]any{"title": "x"}, wantErr: `missing key column "id"`},
		{name: "empty key", sch: single, in: map[string]any{"primary_id": ""}, wantErr: `missing key column "id"`},
		{name: "unknown column", sch: single, in: map[string]any{"primary_id": 1, "nope": 1}, wantErr: `unknown column "nope"`},
		{name: "composite alias", sch: comp, in: map[string]any{"primary_id": 1}, wantErr: "composite key"},
		{name: "composite scalar", sch: comp, in: 1, wantErr: "composite key"},
		{name: "nested value", sch: single, in: map[string]any{"primary_id": map[string]any{}}, wantErr: "must be a scalar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := whereFromValue(tc.sch, tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if gv, ok := got[k]; !ok || gv != v {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestUpdateAttrsStrict(t *testing.T) {
	sch := mustSchema(t, &formSingle{})

	got, err := updateAttrs(sch, map[string]any{"count": 1, "Meta": nil}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got["count"] != 1 {
		t.Fatalf("count = %v", got["count"])
	}
	if v, ok := got["meta"]; !ok || v != nil {
		t.Fatalf("meta = %v, %v; want explicit nil", v, ok)
	}

	for _, key := range []string{"nope", "id"} {
		if _, err := updateAttrs(sch, map[string]any{key: 1}, true); err == nil {
			t.Fatalf("strict %q: want error", key)
		}
		if _, err := updateAttrs(sch, map[string]any{key: 1, "count": 1}, false); err != nil {
			t.Fatalf("lenient %q: %v", key, err)
		}
	}
}

func TestModelFromValue(t *testing.T) {
	for _, in := range []any{formSingle{}, &formSingle{}} {
		m, err := modelFromValue(in)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := m.(*formSingle); !ok {
			t.Fatalf("%T: got %T, want *formSingle", in, m)
		}
	}
	for _, in := range []any{nil, "", 42} {
		if _, err := modelFromValue(in); err == nil {
			t.Fatalf("%v: want error", in)
		}
	}
	if _, err := UpdateFormDB(42); err == nil || !strings.Contains(err.Error(), "unsupported input") {
		t.Fatalf("UpdateFormDB(42) = %v", err)
	}
}

func TestTableValue(t *testing.T) {
	cases := []struct {
		in   map[string]any
		want any
	}{
		{map[string]any{"table": "user_bank"}, "user_bank"},
		{map[string]any{"tablename": "user_bank"}, "user_bank"},
		{map[string]any{"table": "a", "tablename": "b"}, "a"},
		{map[string]any{}, nil},
	}
	for _, tc := range cases {
		if got := tableValue(tc.in); got != tc.want {
			t.Fatalf("tableValue(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

type modelForTest struct {
	Id uint32
}

func TestNewBaseForm_NilRequest(t *testing.T) {
	frm := NewBaseForm[modelForTest](nil)
	if !frm.IsNewRecord || frm.PrimaryId != 0 {
		t.Errorf("nil request: want new record, got IsNewRecord=%v PrimaryId=%d", frm.IsNewRecord, frm.PrimaryId)
	}
	if frm.Form == nil {
		t.Error("nil request: Form must be a non-nil map")
	}
	if frm.Module != "modelForTest" {
		t.Errorf("Module: got %q", frm.Module)
	}
}

func TestNewBaseForm_BodyOverridesQuery(t *testing.T) {
	in := &Request{Query: "user_id=1&item_id=2&module=Q", Form: []byte(`{"user_id":10,"item_id":"20","module":"Body"}`)}
	frm := NewBaseForm[modelForTest](in)
	if frm.UserId != 10 || frm.ItemId != 20 || frm.Module != "Body" {
		t.Errorf("got UserId=%d ItemId=%d Module=%q", frm.UserId, frm.ItemId, frm.Module)
	}
}

func TestSetParams_UpdatesQuery(t *testing.T) {
	in := &Request{Query: "primary_id=5"}
	frm := NewBaseForm[modelForTest](in)
	frm.SetParams(map[string]any{"primary_id": 9, "item_id": 3})
	if frm.PrimaryId != 9 || frm.IsNewRecord || frm.ItemId != 3 {
		t.Errorf("got PrimaryId=%d IsNewRecord=%v ItemId=%d", frm.PrimaryId, frm.IsNewRecord, frm.ItemId)
	}
	if frm.Query("primary_id") != "9" {
		t.Errorf("Query(primary_id): got %q", frm.Query("primary_id"))
	}
	if got := NewBaseForm[modelForTest](&Request{Query: frm.QueryString()}).PrimaryId; got != 9 {
		t.Errorf("query string not updated: nested form PrimaryId=%d", got)
	}
}

func TestPkLookup(t *testing.T) {
	frm := NewBaseForm[modelForTest](&Request{})
	if _, _, ok := frm.pkLookup(); ok {
		t.Error("new record: want no lookup")
	}
	frm.PrimaryId = 7
	if q, args, ok := frm.pkLookup(); !ok || q != "id = ?" || args[0] != uint32(7) {
		t.Errorf("scalar: got %q %v %v", q, args, ok)
	}
	frm.PKColumn, frm.PKValue = "a = ? AND b = ?", []any{1, 0}
	if _, _, ok := frm.pkLookup(); ok {
		t.Error("composite with empty part: want no lookup")
	}
	frm.PKValue = []any{1, 2}
	if q, args, ok := frm.pkLookup(); !ok || q != "a = ? AND b = ?" || len(args) != 2 {
		t.Errorf("composite: got %q %v %v", q, args, ok)
	}
}

func TestMapper(t *testing.T) {
	m := ToMapped([]byte(`{"s":"x","n":0,"e":"","l":["a","",2],"o":{},"one":"z"}`))
	if !m.Has("n") || m.ToString("n") != "" || m.Raw("n") == nil {
		t.Error("zero number: Has/Raw keep it, ToString treats it as empty")
	}
	if got := m.ToList("l"); len(got) != 2 || got[0] != "a" || got[1] != "2" {
		t.Errorf("ToList: got %v", got)
	}
	if got := m.ToList("one"); len(got) != 1 || got[0] != "z" {
		t.Errorf("ToList single: got %v", got)
	}
	if m.ToList("missing") != nil || m.ToMap("s") != nil || m.ToSlice("s") != nil {
		t.Error("wrong-type / missing reads must be nil")
	}
	if m.ToMap("o") == nil {
		t.Error("empty object must be non-nil")
	}
	if ToMapped(nil).ToString("x") != "" {
		t.Error("empty body must read as zero values")
	}
}

func TestNewBaseForm_ZeroPrimaryIdIsNew(t *testing.T) {
	frm := NewBaseForm[modelForTest](&Request{Query: "primary_id=0"})
	if !frm.IsNewRecord {
		t.Error("primary_id=0: want new record")
	}
	frm.SetParams(map[string]any{"primary_id": "abc"})
	if frm.IsNewRecord {
		t.Error("string primary_id: want existing record")
	}
	frm.SetParams(map[string]any{"primary_id": 0})
	if !frm.IsNewRecord {
		t.Error("SetParams primary_id=0: want new record")
	}
}
