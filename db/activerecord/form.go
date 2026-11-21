package activerecord

import (
	"context"
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

// FormRequest is the slice of the host's gRPC *Request that UpdateFormData
// needs. Any generated proto message with Query/Form fields satisfies it, so
// this package never imports the host's protos.
type FormRequest interface {
	GetQuery() string
	GetForm() []byte
}

var tableModelCache sync.Map // map[string]string — table name / struct name → registry key

// UpdateFormData updates one row of a registered model from a generic form
// request and returns the reloaded model (a *T as any).
//
// Expected request shape:
//
//	query: ?primary_id=42                  (or "primary_id" in the form)
//	form:  {"tablename": "user_profile", "data": {"first_name": "Ann"}}
//
// "tablename" may be the DB table name or the Go struct name, and must belong
// to a model registered with RegisterModel — unregistered tables are refused,
// so a client can't write to arbitrary tables. "data" may be a JSON object or
// a JSON-encoded string. Keys are matched against the model's columns (DB
// name or Go field name); unknown, primary-key and non-updatable columns are
// dropped. The model's UpdateHooks run before the write, as in SaveDB.
//
// Pass a transaction to run the update inside it.
func UpdateFormDB(in FormRequest, tx ...*gorm.DB) (any, error) {
	form := lib.ToMapped(in.GetForm())
	if form == nil {
		return nil, errors.New("UpdateFormData: invalid form")
	}

	tablename := strings.TrimSpace(lib.ToString(form["tablename"]))
	if tablename == "" {
		return nil, errors.New("UpdateFormData: tablename is required")
	}
	model, err := modelForTable(tablename)
	if err != nil {
		return nil, err
	}

	primaryID := queryParam(in.GetQuery(), "primary_id")
	if primaryID == "" {
		primaryID = strings.TrimSpace(lib.ToString(form["primary_id"]))
	}
	if primaryID == "" {
		return nil, errors.New("UpdateFormData: primary_id is required")
	}

	data, err := formData(form["data"])
	if err != nil {
		return nil, err
	}

	sch, err := parseSchema(model)
	if err != nil {
		return nil, fmt.Errorf("UpdateFormData[%s]: %w", tablename, err)
	}
	if len(sch.PrimaryFields) != 1 {
		return nil, fmt.Errorf("UpdateFormData[%s]: model must have exactly one primary key", tablename)
	}
	pkCol := sch.PrimaryFields[0].DBName

	attrs := make(map[string]any, len(data))
	for key, val := range data {
		f := sch.LookUpField(key)
		if f == nil || f.PrimaryKey || !f.Updatable || f.DBName == "" {
			continue
		}
		attrs[f.DBName] = val
	}
	if len(attrs) == 0 {
		return nil, fmt.Errorf("UpdateFormData[%s]: no updatable fields in data", tablename)
	}

	db := DB(tx...)
	where := fmt.Sprintf("`%s` = ?", pkCol)
	if err := db.Where(where, primaryID).First(model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("UpdateFormData[%s]: record %s not found", tablename, primaryID)
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
		return nil, fmt.Errorf("UpdateFormData[%s]: %w", tablename, err)
	}

	fresh := reflect.New(modelType).Interface()
	if err := db.Where(where, primaryID).First(fresh).Error; err != nil {
		return nil, err
	}
	return fresh, nil
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
	return nil, fmt.Errorf("UpdateFormData: unknown table %q", name)
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
		if err := json.Unmarshal([]byte(d), &m); err != nil {
			return nil, fmt.Errorf("UpdateFormData: data is not a JSON object: %w", err)
		}
		return m, nil
	case nil:
		return nil, errors.New("UpdateFormData: data is required")
	default:
		return nil, fmt.Errorf("UpdateFormData: data must be an object, got %T", v)
	}
}

func queryParam(rawQuery, key string) string {
	// ParseQuery keeps every well-formed pair even when it returns an error.
	vals, _ := url.ParseQuery(strings.TrimPrefix(rawQuery, "?"))
	return strings.TrimSpace(vals.Get(key))
}
