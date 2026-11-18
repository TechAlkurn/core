package base

import (
	"testing"
)

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
