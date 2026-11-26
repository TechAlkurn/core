package activerecord

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"

	"github.com/TechAlkurn/core/db/hooks"
	"github.com/TechAlkurn/core/lib"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// FormRequest is the slice of the host's gRPC *Request that UpdateFormDB
// needs. Any generated proto message with Query/Form fields satisfies it, so
// this package never imports the host's protos.
type FormRequest interface {
	GetQuery() string
	GetForm() []byte
}

var tableModelCache sync.Map // map[string]string — table name / struct name → registry key

// UpdateFormDB updates one row of a registered model from a generic form
// request and returns the reloaded model (a *T as any).
//
// Expected request shape:
//
//	query: ?primary_id=42
//	form:  {"table": "user_profile", "data": {"first_name": "Ann", "meta": {"a": 1}}}
//
// "table" ("tablename" is still accepted) may be the DB table name or the Go
// struct name, and must belong
// to a model registered with RegisterModel — unregistered tables are refused,
// so a client can't write to arbitrary tables.
//
// The row is identified by "primary_id", read from the query or the form:
//   - a scalar (`primary_id=42`) when the model has a single primary key;
//   - an object keyed by column or Go field name for composite keys, either
//     in the form (`"primary_id": {"user_id": 1, "item_name": "admin"}`) or
//     JSON-encoded in the query;
//   - or, when "primary_id" is absent, one query/form param per key column
//     (`?user_id=1&item_name=admin`).
//
// Every primary-key column must be given.
//
// "data" may be a JSON object or a JSON-encoded string. Keys are matched
// against the model's columns (DB name or Go field name); unknown,
// primary-key and non-updatable columns are dropped. Nested objects/arrays
// are converted for the column's Go type (see columnValue). The model's
// UpdateHooks run before the write, as in SaveDB.
//
// Pass a transaction to run the update inside it.
/*
┌────────────────────────────────────────────────────────────────┬─────────────────────────────────────────────────────────────┐
│                            Request                             │                             Use                             │
├────────────────────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────┤
│ ?primary_id=42 or "primary_id": 42 in the form                 │ single-column key (works as before)                         │
├────────────────────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────┤
│ "primary_id": {"user_id": 1, "item_name": "admin"} in the form │ composite key; names can be the column or the Go field name │
├────────────────────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────┤
│ ?primary_id={"user_id":1,"item_name":"admin"} (URL-encoded)    │ composite key in the query                                  │
├────────────────────────────────────────────────────────────────┼─────────────────────────────────────────────────────────────┤
│ ?user_id=1&item_name=admin, with no primary_id                 │ one parameter per key column                                │
└────────────────────────────────────────────────────────────────┴─────────────────────────────────────────────────────────────┘
*/
//
// Server-side form: pass a map[string]any instead of a request.
//
//	activerecord.UpdateFormDB(map[string]any{
//		"table":      "user_bank", // or "UserBank" (registered model), or model.UserBank{}
//		"fields":     map[string]any{"is_approved": 1, "otp": nil},
//		"primary_id": map[string]any{"primary_id": primaryId, "otp": otp},
//	})
//
//   - "table" (or "tablename"): a model value (T, *T) — no registration
//     needed — or a table/struct name of a registered model.
//   - "fields" (or "data"): columns to set; nil writes NULL. Unknown or
//     non-updatable columns are an error rather than silently dropped.
//   - "primary_id": a scalar (single-column key) or a WHERE map. Inside the
//     map, "primary_id" stands for the single key column and any other key
//     is a column (DB or Go field name); nil matches IS NULL. Every primary
//     key column must be covered, so one call updates at most one row.
//     Extra columns (e.g. "otp") are also applied to the UPDATE itself, so a
//     concurrent change between the read and the write can't slip through.
//
// When no row matches, the error wraps gorm.ErrRecordNotFound.
func UpdateFormDB(in any, tx ...*gorm.DB) (any, error) {
	switch v := in.(type) {
	case map[string]any:
		return updateFromMap(v, tx...)
	case FormRequest:
		return updateFromRequest(v, tx...)
	default:
		return nil, fmt.Errorf("UpdateFormDB: unsupported input %T", in)
	}
}

func updateFromRequest(in FormRequest, tx ...*gorm.DB) (any, error) {
	form, err := decodeForm(in.GetForm())
	if err != nil {
		return nil, err
	}

	tablename := strings.TrimSpace(lib.ToString(tableValue(form)))
	if tablename == "" {
		return nil, errors.New("UpdateFormDB: table is required")
	}
	model, err := modelForTable(tablename)
	if err != nil {
		return nil, err
	}

	sch, err := parseSchema(model)
	if err != nil {
		return nil, fmt.Errorf("UpdateFormDB[%s]: %w", tablename, err)
	}

	pk, err := primaryKeyFromRequest(sch, in.GetQuery(), form)
	if err != nil {
		return nil, fmt.Errorf("UpdateFormDB[%s]: %w", tablename, err)
	}

	data, err := formData(form["data"])
	if err != nil {
		return nil, err
	}

	attrs, err := updateAttrs(sch, data, false)
	if err != nil {
		return nil, fmt.Errorf("UpdateFormDB[%s]: %w", tablename, err)
	}
	return applyUpdate(model, sch, sch.Table, pk, attrs, tx...)
}

func updateFromMap(in map[string]any, tx ...*gorm.DB) (any, error) {
	model, err := modelFromValue(tableValue(in))
	if err != nil {
		return nil, err
	}
	sch, err := parseSchema(model)
	if err != nil {
		return nil, fmt.Errorf("UpdateFormDB: %w", err)
	}
	name := sch.Table

	raw, ok := in["fields"]
	if !ok {
		raw = in["data"]
	}
	data, err := formData(raw)
	if err != nil {
		return nil, err
	}
	attrs, err := updateAttrs(sch, data, true)
	if err != nil {
		return nil, fmt.Errorf("UpdateFormDB[%s]: %w", name, err)
	}

	where, err := whereFromValue(sch, in["primary_id"])
	if err != nil {
		return nil, fmt.Errorf("UpdateFormDB[%s]: %w", name, err)
	}
	return applyUpdate(model, sch, name, where, attrs, tx...)
}

// tableValue returns m["table"], falling back to the older "tablename" key.
func tableValue(m map[string]any) any {
	if v, ok := m["table"]; ok && v != nil {
		return v
	}
	return m["tablename"]
}

// modelFromValue returns a new *T for "table": a model value (T or *T) or
// the table/struct name of a registered model.
func modelFromValue(v any) (any, error) {
	switch t := v.(type) {
	case nil:
		return nil, errors.New("UpdateFormDB: table is required")
	case string:
		name := strings.TrimSpace(t)
		if name == "" {
			return nil, errors.New("UpdateFormDB: table is required")
		}
		return modelForTable(name)
	}
	rt := reflect.TypeOf(v)
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil, fmt.Errorf("UpdateFormDB: table must be a model or a table name, got %T", v)
	}
	return reflect.New(rt).Interface(), nil
}

// updateAttrs maps data keys (DB or Go field names) to DB columns. Primary
// keys and non-updatable columns are never written; in strict mode they and
// unknown keys are an error instead of being skipped.
func updateAttrs(sch *schema.Schema, data map[string]any, strict bool) (map[string]any, error) {
	attrs := make(map[string]any, len(data))
	for key, val := range data {
		f := sch.LookUpField(key)
		if f == nil || f.PrimaryKey || !f.Updatable || f.DBName == "" {
			if strict {
				return nil, fmt.Errorf("fields: %q is not an updatable column", key)
			}
			continue
		}
		v, err := columnValue(f, val)
		if err != nil {
			return nil, err
		}
		attrs[f.DBName] = v
	}
	if len(attrs) == 0 {
		return nil, errors.New("no updatable fields in data")
	}
	return attrs, nil
}

// whereFromValue builds the WHERE map for the map form of UpdateFormDB. See
// UpdateFormDB for the accepted shapes.
func whereFromValue(sch *schema.Schema, v any) (map[string]any, error) {
	if len(sch.PrimaryFields) == 0 {
		return nil, errors.New("model has no primary key")
	}
	where := map[string]any{}
	switch m := v.(type) {
	case nil:
		return nil, errors.New("primary_id is required")
	case map[string]any:
		for key, val := range m {
			switch val.(type) {
			case map[string]any, []any:
				return nil, fmt.Errorf("primary_id: %q must be a scalar", key)
			}
			if f := sch.LookUpField(key); f != nil && f.DBName != "" {
				where[f.DBName] = val
				continue
			}
			if key != "primary_id" {
				return nil, fmt.Errorf("primary_id: unknown column %q", key)
			}
			if len(sch.PrimaryFields) != 1 {
				return nil, fmt.Errorf("primary_id: composite key %s needs each key column by name", pkColumns(sch))
			}
			where[sch.PrimaryFields[0].DBName] = val
		}
	case []any:
		return nil, errors.New("primary_id must be a scalar or a map")
	default:
		if len(sch.PrimaryFields) != 1 {
			return nil, fmt.Errorf("primary_id: composite key %s needs a map", pkColumns(sch))
		}
		where[sch.PrimaryFields[0].DBName] = m
	}
	for _, f := range sch.PrimaryFields {
		if val, ok := where[f.DBName]; !ok || isBlank(val) {
			return nil, fmt.Errorf("primary_id: missing key column %q (key is %s)", f.DBName, pkColumns(sch))
		}
	}
	return where, nil
}

// applyUpdate loads the row matching where into model, runs the model's
// UpdateHooks, writes attrs (still guarded by where) and returns the row
// reloaded by its primary key.
func applyUpdate(model any, sch *schema.Schema, name string, where, attrs map[string]any, tx ...*gorm.DB) (any, error) {
	db := DB(tx...)
	if err := db.Where(where).First(model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("UpdateFormDB[%s]: record %v not found: %w", name, where, err)
		}
		return nil, err
	}

	rv := reflect.ValueOf(model).Elem()
	if fn, ok := hooks.UpdateHooks[rv.Type()]; ok {
		fn(model)
		// Sync the hook's updated_at into attrs so it is written too.
		if f := sch.LookUpField("updated_at"); f != nil {
			attrs[f.DBName] = f.ReflectValueOf(context.Background(), rv).Interface()
		}
	}

	// Reload by primary key only: attrs may change the other where columns
	// (e.g. clearing otp), so the original where would no longer match.
	pk := make(map[string]any, len(sch.PrimaryFields))
	for _, f := range sch.PrimaryFields {
		pk[f.DBName] = f.ReflectValueOf(context.Background(), rv).Interface()
	}

	if err := db.Model(model).Where(where).Updates(attrs).Error; err != nil {
		logLockWaitDiagnostics(err)
		return nil, fmt.Errorf("UpdateFormDB[%s]: %w", name, err)
	}

	fresh := reflect.New(rv.Type()).Interface()
	if err := db.Where(pk).First(fresh).Error; err != nil {
		return nil, err
	}
	return fresh, nil
}

// decodeForm decodes the request form with UseNumber so large integer IDs
// keep full precision instead of passing through float64.
func decodeForm(b []byte) (map[string]any, error) {
	var form map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&form); err != nil || form == nil {
		return nil, errors.New("UpdateFormDB: invalid form")
	}
	return form, nil
}

// primaryKeyFromRequest builds a column → value map covering every primary
// key column of sch, from "primary_id" (scalar or object) or from one param
// per key column. See UpdateFormDB for the accepted shapes.
func primaryKeyFromRequest(sch *schema.Schema, rawQuery string, form map[string]any) (map[string]any, error) {
	if len(sch.PrimaryFields) == 0 {
		return nil, errors.New("model has no primary key")
	}
	query, _ := url.ParseQuery(strings.TrimPrefix(rawQuery, "?")) // keeps well-formed pairs on error

	var raw any
	if v := strings.TrimSpace(query.Get("primary_id")); v != "" {
		raw = v
	} else if v, ok := form["primary_id"]; ok && !isBlank(v) {
		raw = v
	}

	// A JSON object given as a string (typically from the query).
	if s, ok := raw.(string); ok && strings.HasPrefix(s, "{") {
		obj := map[string]any{}
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			return nil, fmt.Errorf("primary_id is not a valid JSON object: %w", err)
		}
		raw = obj
	}

	pk := make(map[string]any, len(sch.PrimaryFields))
	switch v := raw.(type) {
	case nil:
		// No primary_id: one param per key column, query first, then form.
		for _, f := range sch.PrimaryFields {
			if qv := strings.TrimSpace(query.Get(f.DBName)); qv != "" {
				pk[f.DBName] = qv
			} else if fv, ok := form[f.DBName]; ok && !isBlank(fv) {
				pk[f.DBName] = fv
			}
		}
		if len(pk) == 0 {
			return nil, errors.New("primary_id is required")
		}
	case map[string]any:
		for key, val := range v {
			f := sch.LookUpField(key)
			if f == nil || !f.PrimaryKey {
				return nil, fmt.Errorf("primary_id: %q is not a primary key column", key)
			}
			if isBlank(val) {
				return nil, fmt.Errorf("primary_id: %q is empty", key)
			}
			if _, nested := val.(map[string]any); nested {
				return nil, fmt.Errorf("primary_id: %q must be a scalar", key)
			}
			if _, nested := val.([]any); nested {
				return nil, fmt.Errorf("primary_id: %q must be a scalar", key)
			}
			pk[f.DBName] = val
		}
	case []any:
		return nil, errors.New("primary_id must be a scalar or an object")
	default:
		if len(sch.PrimaryFields) != 1 {
			return nil, fmt.Errorf("primary_id: composite key %s needs an object, e.g. {%q: ...}",
				pkColumns(sch), sch.PrimaryFields[0].DBName)
		}
		pk[sch.PrimaryFields[0].DBName] = v
	}

	for _, f := range sch.PrimaryFields {
		if _, ok := pk[f.DBName]; !ok {
			return nil, fmt.Errorf("primary_id: missing key column %q (key is %s)", f.DBName, pkColumns(sch))
		}
	}
	return pk, nil
}

func pkColumns(sch *schema.Schema) string {
	cols := make([]string, len(sch.PrimaryFields))
	for i, f := range sch.PrimaryFields {
		cols[i] = f.DBName
	}
	return "(" + strings.Join(cols, ", ") + ")"
}

func isBlank(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

var valuerType = reflect.TypeFor[driver.Valuer]()

// columnValue prepares one "data" value for a map-based Updates. Scalars pass
// through unchanged. Nested objects/arrays (map[string]any / []any) are
// converted according to the column's Go type, since GORM's map Updates
// sends map values to the driver as-is and never runs field serializers:
//
//   - types implementing driver.Valuer (datatypes.JSON, custom JSON types):
//     decoded into that type, so its Value() produces the stored form;
//   - []byte / json.RawMessage: the JSON bytes;
//   - string, `serializer:json` fields and plain struct/map/slice fields
//     (JSON columns): the JSON text, after checking it decodes into the
//     field's Go type so a shape mismatch is reported instead of stored.
func columnValue(f *schema.Field, val any) (any, error) {
	switch val.(type) {
	case map[string]any, []any:
	default:
		return val, nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("data.%s: %w", f.DBName, err)
	}

	target := f.FieldType
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}

	if f.Serializer == nil && (target.Implements(valuerType) || reflect.PointerTo(target).Implements(valuerType)) {
		ptr := reflect.New(target)
		if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
			return nil, fmt.Errorf("data.%s: %w", f.DBName, err)
		}
		if target.Implements(valuerType) {
			return ptr.Elem().Interface(), nil
		}
		return ptr.Interface(), nil
	}

	switch {
	case target.Kind() == reflect.Slice && target.Elem().Kind() == reflect.Uint8:
		return raw, nil
	case target.Kind() == reflect.String:
		return string(raw), nil
	case target.Kind() == reflect.Struct, target.Kind() == reflect.Map,
		target.Kind() == reflect.Slice, target.Kind() == reflect.Array,
		target.Kind() == reflect.Interface:
		if err := json.Unmarshal(raw, reflect.New(target).Interface()); err != nil {
			return nil, fmt.Errorf("data.%s: %w", f.DBName, err)
		}
		return string(raw), nil
	default:
		return nil, fmt.Errorf("data.%s: column of type %s can't hold an object or array", f.DBName, f.FieldType)
	}
}

// modelForTable returns a new *T for the registered model whose table name or
// struct name equals name.
func modelForTable(name string) (any, error) {
	if key, ok := tableModelCache.Load(name); ok {
		if m, ok := ActiveModel(key.(string)); ok {
			return m, nil
		}
	}
	if m, ok := ActiveModel(name); ok {
		tableModelCache.Store(name, name)
		return m, nil
	}

	registryMu.RLock()
	keys := make([]string, 0, len(modelRegistry))
	for k := range modelRegistry {
		keys = append(keys, k)
	}
	registryMu.RUnlock()

	for _, key := range keys {
		m, ok := ActiveModel(key)
		if !ok {
			continue
		}
		sch, err := parseSchema(m)
		if err != nil || sch.Table != name {
			continue
		}
		tableModelCache.Store(name, key)
		return m, nil
	}
	return nil, fmt.Errorf("UpdateFormDB: unknown table %q", name)
}

// parseSchema parses model with GORM (which caches the schema per type) so
// the table name honours TableName() and the naming strategy.
func parseSchema(model any) (*schema.Schema, error) {
	stmt := &gorm.Statement{DB: DB()}
	if err := stmt.Parse(model); err != nil {
		return nil, err
	}
	return stmt.Schema, nil
}

// formData accepts the "data" value as a JSON object or a JSON-encoded string.
func formData(v any) (map[string]any, error) {
	switch d := v.(type) {
	case map[string]any:
		return d, nil
	case string:
		var m map[string]any
		dec := json.NewDecoder(strings.NewReader(d))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("UpdateFormDB: data is not a JSON object: %w", err)
		}
		return m, nil
	case nil:
		return nil, errors.New("UpdateFormDB: data is required")
	default:
		return nil, fmt.Errorf("UpdateFormDB: data must be an object, got %T", v)
	}
}
