// Package base holds the domain-free core of every request form: request
// parsing, primary-key lookup, the generic save path and small helpers.
// Anything that touches application models (custom fields, media, enums)
// lives in pkg/application/common/forms, which embeds BaseForm.
package base

import (
	"errors"
	"maps"
	"reflect"
	"slices"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/db/activerecord"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/lib"
	"gorm.io/gorm"
)

// BaseForm is the generic form for model T. Construct it with NewBaseForm.
type BaseForm[T any] struct {
	ModelItem   *activerecord.ActiveRecord[T]
	Req         *action.Context
	Mapper      Mapper
	Form        map[string]any // decoded request body; same map as Mapper.Data()
	IsNewRecord bool
	PrimaryId   uint32
	Module      string
	UserId      uint32
	ItemId      uint32
	// PKColumn is the DB column used to look up an existing record by PrimaryId.
	// PKField is the Go struct field on T whose value gets synced back into
	// frm.PrimaryId after a successful save. Both default to "id"/"Id"; a child
	// form overrides them in its constructor when the model uses a different PK.
	// PKValue overrides the lookup value entirely — used for string PKs where
	// frm.PrimaryId (uint32) can't carry the value. When nil, PrimaryId is used.
	// For a composite PK, PKColumn is the full clause
	// ("user_id = ? AND catalog_id = ?") and PKValue a []any of bind values.
	PKColumn string
	PKField  string
	PKValue  any
	// AfterSave runs inside the save transaction, after SaveItem persists the
	// row but before commit. Return an error to trigger a rollback. Useful for
	// creating linked rows (mail templates, audit logs, etc.) atomically with
	// the primary save.
	AfterSave func(*T) error

	tx *gorm.DB
}

// NewBaseForm parses in (query + JSON body) once and binds the form to tx
// when given. A nil in (e.g. from cron) yields an empty new-record form.
func NewBaseForm[T any](in Payload, tx ...*gorm.DB) *BaseForm[T] {
	req := action.Request(queryOf(in))
	mapper := ToMapped(formOf(in))
	primaryID := req.Query("primary_id")

	frm := &BaseForm[T]{
		ModelItem:   activerecord.NewAR[T](),
		Req:         req,
		Mapper:      mapper,
		Form:        mapper.Data(),
		PrimaryId:   lib.ToUint32(primaryID),
		IsNewRecord: lib.IsEmpty(primaryID),
		UserId:      lib.ToUint32(req.Query("user_id")),
		ItemId:      lib.ToUint32(req.Query("item_id")),
		Module:      req.Query("module"),
		PKColumn:    "id",
		PKField:     "Id",
	}
	if frm.Form == nil {
		frm.Form = map[string]any{}
		frm.Mapper = Mapper{data: frm.Form}
	}
	if frm.Module == "" {
		frm.Module = reflect.TypeFor[T]().Name()
	}
	// Body values override the query string.
	if mapper.Has("user_id") {
		frm.UserId = mapper.ToUint32("user_id")
	}
	if mapper.Has("item_id") {
		frm.ItemId = mapper.ToUint32("item_id")
	}
	if mapper.Has("module") {
		frm.Module = mapper.ToString("module")
	}
	if len(tx) > 0 && tx[0] != nil {
		frm.tx = tx[0]
	}
	return frm
}

// Tx returns the transaction handle the form is bound to (nil = autocommit).
func (frm *BaseForm[T]) Tx() *gorm.DB { return frm.tx }

// QueryString returns the current query string, including any params merged
// by SetParams — use it to build a nested form that sees the same params.
func (frm *BaseForm[T]) QueryString() string { return frm.Req.ToQueryString() }

// Query returns a query-string parameter, including any set via SetParams.
func (frm *BaseForm[T]) Query(key string) string {
	return frm.Req.Query(key)
}

// SetParams merges params into the request query string (read it back with
// QueryString) and re-derives PrimaryId, IsNewRecord, UserId, ItemId and
// Module from it.
func (frm *BaseForm[T]) SetParams(params map[string]any) *BaseForm[T] {
	frm.Req.SetQueryParams(params)
	primaryID := frm.Req.Query("primary_id")
	frm.PrimaryId = lib.ToUint32(primaryID)
	frm.IsNewRecord = lib.IsEmpty(primaryID)
	if v := frm.Req.Query("user_id"); v != "" {
		frm.UserId = lib.ToUint32(v)
	}
	if v := frm.Req.Query("item_id"); v != "" {
		frm.ItemId = lib.ToUint32(v)
	}
	if v := frm.Req.Query("module"); v != "" {
		frm.Module = v
	}
	return frm
}

// GetForm reads key from the body and writes it into dest, converting to
// dest's underlying type. dest must be a non-nil pointer (e.g. &frm.Email) —
// a plain method can't return a different concrete type per call, so the
// destination's type drives the conversion instead.
func (frm *BaseForm[T]) GetForm(key string, dest any) {
	raw := frm.Form[key]
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	elem := v.Elem()
	switch elem.Kind() {
	case reflect.String:
		elem.SetString(lib.ToString(raw))
	case reflect.Bool:
		elem.SetBool(lib.ToBool(raw))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		elem.SetInt(lib.ToInt64(raw))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		elem.SetUint(lib.ToUint64(raw))
	case reflect.Float32, reflect.Float64:
		elem.SetFloat(lib.ToFloat64(raw))
	default:
		if rv := reflect.ValueOf(raw); rv.IsValid() && rv.Type().AssignableTo(elem.Type()) {
			elem.Set(rv)
		}
	}
}

// FindOne returns the matching row, or a fresh zero-value T when none matches.
func (frm *BaseForm[T]) FindOne(query any, args ...any) *T {
	entity, _ := frm.ModelItem.FindOne(query, args...)
	if entity == nil {
		entity = new(T)
	}
	return entity
}

// pkLookup builds the WHERE clause and bind args for the primary-key lookup.
// ok is false when there's nothing to look up yet (new record, or any part
// of a composite key empty), so callers skip the DB round-trip.
func (frm *BaseForm[T]) pkLookup() (query string, args []any, ok bool) {
	if values, isComposite := frm.PKValue.([]any); isComposite {
		if len(values) == 0 || slices.ContainsFunc(values, lib.IsEmpty) {
			return "", nil, false
		}
		return frm.PKColumn, values, true
	}
	lookup := frm.PKValue
	if lookup == nil {
		lookup = frm.PrimaryId
	}
	if lib.IsEmpty(lookup) {
		return "", nil, false
	}
	column := frm.PKColumn
	if column == "" {
		column = "id"
	}
	return column + " = ?", []any{lookup}, true
}

// FindItem returns the record being edited, or a fresh zero-value T without
// touching the DB when this is a new record.
func (frm *BaseForm[T]) FindItem(arg ...any) *T {
	query, args, ok := frm.pkLookup()
	if !ok {
		return new(T)
	}
	return frm.FindOne(query, args...)
}

// findItemErr is FindItem for the save path: "not found" still yields a
// fresh T, but any other DB error is returned so SaveItem never turns a
// failed lookup into an INSERT.
func (frm *BaseForm[T]) findItemErr() (*T, error) {
	query, args, ok := frm.pkLookup()
	if !ok {
		return new(T), nil
	}
	entity, err := frm.ModelItem.FindOne(query, args...)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return new(T), nil
	}
	if err != nil {
		return nil, err
	}
	return entity, nil
}

// SaveItem upserts the body into T (insert for a new record, update
// otherwise), syncs the saved primary key back into PrimaryId, then runs
// AfterSave.
func (frm *BaseForm[T]) SaveItem() (modelItem *T, err error) {
	existing, err := frm.findItemErr()
	if err != nil {
		return nil, err
	}
	modelItem, err = frm.ModelItem.SaveDB(existing, frm.Form, frm.tx)
	if err != nil {
		return nil, err
	}
	if modelItem != nil {
		if v := reflect.ValueOf(modelItem).Elem().FieldByName(frm.PKField); v.IsValid() && v.CanUint() {
			// IsNewRecord is left as-is: child forms read it after SaveItem
			// to decide on create-only work.
			frm.PrimaryId = uint32(v.Uint())
		}
	}
	if frm.AfterSave != nil {
		err = frm.AfterSave(modelItem)
	}
	return modelItem, err
}

// SaveIfPresents runs handlers[key] for every key sent in the body, in key
// order, stopping at the first error.
func (frm *BaseForm[T]) SaveIfPresents(handlers map[string]func(any) error) error {
	for _, key := range slices.Sorted(maps.Keys(handlers)) {
		handler := handlers[key]
		item, ok := frm.Form[key]
		if !ok {
			continue
		}
		if err := handler(item); err != nil {
			config.Log.Printf("SaveIfPresents[%s]: %v\n", key, err)
			return err
		}
	}
	return nil
}

// SaveIfPresentsWithDefault is SaveIfPresents, but a key missing from the
// body runs its handler with defaultValue instead of being skipped. Handlers
// run in key order.
func (frm *BaseForm[T]) SaveIfPresentsWithDefault(handlers map[string]func(any) error, defaultValue any) error {
	for _, key := range slices.Sorted(maps.Keys(handlers)) {
		handler := handlers[key]
		item, ok := frm.Form[key]
		if !ok {
			item = defaultValue
		}
		if err := handler(item); err != nil {
			config.Log.Printf("SaveIfPresentsWithDefault[%s]: %v\n", key, err)
			return err
		}
	}
	return nil
}

// SyncChildren replaces every child row referencing a parent (fk = fkValue)
// with one row per item in items ([]any of map[string]any, each stamped with
// fk = fkValue). The delete is skipped for a new parent — nothing to replace
// yet. Non-object items are ignored.
func SyncChildren[T any](tx *gorm.DB, isNewRecord bool, fk string, fkValue uint32, items any) error {
	list, _ := items.([]any)
	ar := activerecord.NewAR[T]()
	if !isNewRecord {
		if err := ar.DeleteAllTx(tx, fk+" = ?", fkValue); err != nil {
			return err
		}
	}
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item[fk] = fkValue
		if _, err := ar.Save(item, tx); err != nil {
			config.Log.Printf("SyncChildren[%s]: %v\n", ar.GetModel(), err)
			return err
		}
	}
	return nil
}
