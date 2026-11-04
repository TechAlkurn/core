package hooks

import (
	"reflect"
	"testing"
)

// -----------------------------------------------------------------------------
// setTimestamp — the reflection helper for RegisterTimestampHooks
// -----------------------------------------------------------------------------

type timestampsPlain struct {
	CreatedAt uint32
	UpdatedAt uint32
}

type timestampsPointer struct {
	CreatedAt *uint32
	UpdatedAt *uint32
}

type timestampsWrongType struct {
	CreatedAt string
}

func Test_setTimestamp_Uint32(t *testing.T) {
	m := &timestampsPlain{}
	v := reflect.ValueOf(m).Elem().FieldByName("CreatedAt")
	setTimestamp(v, 12345)
	if m.CreatedAt != 12345 {
		t.Errorf("uint32: got %d want 12345", m.CreatedAt)
	}
}

func Test_setTimestamp_PointerUint32_Allocates(t *testing.T) {
	m := &timestampsPointer{}
	v := reflect.ValueOf(m).Elem().FieldByName("CreatedAt")
	setTimestamp(v, 9999)
	if m.CreatedAt == nil {
		t.Fatal("*uint32: got nil, expected allocated pointer")
	}
	if *m.CreatedAt != 9999 {
		t.Errorf("*uint32: got %d want 9999", *m.CreatedAt)
	}
}

func Test_setTimestamp_WrongType_Skipped(t *testing.T) {
	m := &timestampsWrongType{CreatedAt: "unchanged"}
	v := reflect.ValueOf(m).Elem().FieldByName("CreatedAt")
	setTimestamp(v, 1) // must NOT panic
	if m.CreatedAt != "unchanged" {
		t.Errorf("wrong type: got %q want unchanged", m.CreatedAt)
	}
}

func Test_setTimestamp_MissingField_Skipped(t *testing.T) {
	m := &timestampsPlain{}
	v := reflect.ValueOf(m).Elem().FieldByName("DoesNotExist")
	// Invalid Value — must silently no-op.
	setTimestamp(v, 1)
}

// -----------------------------------------------------------------------------
// RegisterTimestampHooks — end-to-end hook registration + firing
// -----------------------------------------------------------------------------

type hookedModel struct {
	CreatedAt uint32
	UpdatedAt uint32
}

func TestRegisterTimestampHooks_FiresSaveHook(t *testing.T) {
	RegisterTimestampHooks[hookedModel]()

	m := &hookedModel{}
	saveFn, ok := SaveHooks[reflect.TypeFor[hookedModel]()]
	if !ok {
		t.Fatal("SaveHooks not registered for hookedModel")
	}
	saveFn(m)

	if m.CreatedAt == 0 {
		t.Errorf("CreatedAt: got 0, expected non-zero timestamp")
	}
	if m.UpdatedAt == 0 {
		t.Errorf("UpdatedAt: got 0, expected non-zero timestamp")
	}
	if m.CreatedAt != m.UpdatedAt {
		t.Errorf("CreatedAt (%d) != UpdatedAt (%d) on save", m.CreatedAt, m.UpdatedAt)
	}
}

func TestRegisterTimestampHooks_UpdateOnlyTouchesUpdatedAt(t *testing.T) {
	RegisterTimestampHooks[hookedModel]()

	m := &hookedModel{CreatedAt: 111, UpdatedAt: 222}
	updateFn, ok := UpdateHooks[reflect.TypeFor[hookedModel]()]
	if !ok {
		t.Fatal("UpdateHooks not registered for hookedModel")
	}
	updateFn(m)

	if m.CreatedAt != 111 {
		t.Errorf("CreatedAt on update: got %d, expected 111 (unchanged)", m.CreatedAt)
	}
	if m.UpdatedAt == 222 {
		t.Errorf("UpdatedAt on update: unchanged, expected refresh")
	}
}

// -----------------------------------------------------------------------------
// RegisterHooks — panics when T doesn't implement ModelHooks
// -----------------------------------------------------------------------------

type notImplementingModelHooks struct{}

func TestRegisterHooks_PanicsWithoutInterface(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic when T does not implement ModelHooks")
		}
	}()
	RegisterHooks[notImplementingModelHooks]()
}

type implementsModelHooks struct{ saved, updated int }

func (m *implementsModelHooks) OnSave() error   { m.saved++; return nil }
func (m *implementsModelHooks) OnUpdate() error { m.updated++; return nil }

func TestRegisterHooks_SucceedsWithInterface(t *testing.T) {
	RegisterHooks[implementsModelHooks]()

	t2 := reflect.TypeFor[implementsModelHooks]()
	saveFn, ok := SaveHooks[t2]
	if !ok {
		t.Fatal("SaveHooks not registered")
	}
	updateFn, ok := UpdateHooks[t2]
	if !ok {
		t.Fatal("UpdateHooks not registered")
	}

	m := &implementsModelHooks{}
	saveFn(m)
	updateFn(m)
	if m.saved != 1 || m.updated != 1 {
		t.Errorf("dispatch: OnSave called %d, OnUpdate called %d; want 1 each", m.saved, m.updated)
	}

	// ModelHooks has no after-callbacks; those are registered separately via
	// RegisterHookAfterSave / RegisterHookAfterUpdate.
	if _, ok := AfterSaveHooks[t2]; ok {
		t.Error("RegisterHooks must not register an AfterSave hook")
	}
	if _, ok := AfterUpdateHooks[t2]; ok {
		t.Error("RegisterHooks must not register an AfterUpdate hook")
	}
}
