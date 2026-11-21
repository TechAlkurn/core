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
//	form:  {"tablename": "user_profile", "data": {"first_name": "Ann", "meta": {"a": 1}}}
//
// "tablename" may be the DB table name or the Go struct name, and must belong
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
func UpdateFormDB(in FormRequest, tx ...*gorm.DB) (any, error) {
	form, err := decodeForm(in.GetForm())
	if err != nil {
		return nil, err
	}

	tablename := strings.TrimSpace(lib.ToString(form["tablename"]))
	if tablename == "" {
		return nil, errors.New("UpdateFormDB: tablename is required")
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

	attrs := make(map[string]any, len(data))
	for key, val := range data {
		f := sch.LookUpField(key)
		if f == nil || f.PrimaryKey || !f.Updatable || f.DBName == "" {
			continue
		}
		v, err := columnValue(f, val)
		if err != nil {
			return nil, fmt.Errorf("UpdateFormDB[%s]: %w", tablename, err)
		}
		attrs[f.DBName] = v
	}
	if len(attrs) == 0 {
		return nil, fmt.Errorf("UpdateFormDB[%s]: no updatable fields in data", tablename)
	}

	db := DB(tx...)
	if err := db.Where(pk).First(model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("UpdateFormDB[%s]: record %v not found", tablename, pk)
		}
		return nil, err
	}

	modelType := reflect.TypeOf(model).Elem()
	if fn, ok := hooks.UpdateHooks[modelType]; ok {
		fn(model)
		// Sync the hook's updated_at into attrs so it is written too.
		if f := sch.LookUpField("updated_at"); f != nil {
			attrs[f.DBName] = f.ReflectValueOf(context.Background(), reflect.ValueOf(model).Elem()).Interface()
		}
	}

	if err := db.Model(model).Updates(attrs).Error; err != nil {
		logLockWaitDiagnostics(err)
		return nil, fmt.Errorf("UpdateFormDB[%s]: %w", tablename, err)
	}

	fresh := reflect.New(modelType).Interface()
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
