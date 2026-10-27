package activerecord

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"app/pkg/config"
	"app/pkg/core/db/hooks"
	"app/pkg/enum"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/lib"
	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type (
	ModelFactory        func() any
	ActiveRecord[T any] struct{}
	DateTime            struct {
		*time.Time
	}
)

var (
	modelRegistry   = map[string]ModelFactory{}
	registryMu      sync.RWMutex
	pkSchemaCache   sync.Map // map[reflect.Type]map[string]string  — primary key schema per type
	fieldTypeCache  sync.Map // map[reflect.Type]map[string]reflect.Type — field types per type
	tableNameCache  sync.Map // map[reflect.Type]string — resolved table name per type
	isDebugAtomic   atomic.Bool
	isDebugResolved atomic.Bool // false = never resolved; on first call we hit the DB
	reqCache        sync.Map
)

func RegisterModel[T any]() {
	registryMu.Lock()
	defer registryMu.Unlock()
	modelRegistry[reflect.TypeFor[T]().Name()] = func() any {
		return new(T)
	}
}

func ActiveModel(name string) (any, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := modelRegistry[name]
	if !ok {
		return nil, false
	}
	return f(), true
}

// IsDebug reports whether debug/staging DB logging is on. Result is cached in
// process memory forever (atomic.Bool). The first call resolves it via one DB
// query + env-var check; every subsequent call is a lock-free atomic load —
// which matters because DB() calls this on every query.
//
// Replaces the old implementation that hit config.Lcache on every call and
// occasionally re-queried core_config_value on cache expiry.
var IsDebug = func() bool {
	if isDebugResolved.Load() {
		return isDebugAtomic.Load()
	}
	var siteMode string
	config.DB.Raw(" SELECT `value` FROM `core_config_value` WHERE `field_name` = ? ", "site_mode").Scan(&siteMode)
	debug := strings.ToLower(siteMode) == enum.SITE_MODE_STAGING
	debug = debug || os.Getenv("IS_DEBUG") == "true"
	isDebugAtomic.Store(debug)
	isDebugResolved.Store(true)
	return debug
}

// SetIsDebug forces debug/staging DB logging on or off, overriding the
// site_mode / IS_DEBUG resolution. Every subsequent DB() / Begin() call
// picks it up immediately (lock-free atomic store).
//
//	activerecord.SetIsDebug(true)
//	defer activerecord.SetIsDebug(false)
func SetIsDebug(debug bool) {
	isDebugAtomic.Store(debug)
	isDebugResolved.Store(true)
}

func QueryFunc(fn func()) func() (any, error) {
	return func() (any, error) {
		fn()
		return nil, nil
	}
}

func NewDateTime(t time.Time) *DateTime {
	if lib.IsEmpty(t) {
		return nil
	}
	return &DateTime{Time: &t}
}

func (dt *DateTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "null" || s == "" {
		dt.Time = nil
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			dt.Time = &t
			return nil
		}
	}
	// Lenient fallback: if the value contains a valid date prefix (YYYY-MM-DD),
	// accept just the date portion rather than aborting the whole request on
	// malformed time segments (e.g. "2026-04-21 : AM:00").
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			dt.Time = &t
			return nil
		}
	}
	dt.Time = nil
	return nil
}

func (dt DateTime) Value() (driver.Value, error) {
	if dt.Time == nil {
		return nil, nil
	}
	return *dt.Time, nil
}

// MarshalJSON emits the same `2006-01-02 15:04:05` layout that UnmarshalJSON
// accepts, so requests and responses are symmetric. Without this the embedded
// *time.Time falls back to RFC3339 (`2026-05-31T05:30:00+05:30`).
func (dt DateTime) MarshalJSON() ([]byte, error) {
	if dt.Time == nil {
		return []byte("null"), nil
	}
	return []byte(`"` + dt.Time.Format("2006-01-02 15:04:05") + `"`), nil
}

func (dt *DateTime) Scan(value any) error {
	if value == nil {
		dt.Time = nil
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		dt.Time = &v
	case *time.Time:
		dt.Time = v
	default:
		return fmt.Errorf("DateTime.Scan: unsupported type %T", value)
	}
	return nil
}

func ToDateTime(input any) *DateTime {
	if input == nil {
		now := time.Now()
		return &DateTime{Time: &now}
	}
	t := lib.ToTime(input)
	if t == nil {
		return nil
	}
	return &DateTime{Time: t}
}

func Begin() *gorm.DB {
	db := config.DB
	if IsDebug() {
		db = db.Debug()
	}
	return db.Begin()
}

func DB(tx ...*gorm.DB) *gorm.DB {
	db := config.DB
	if len(tx) > 0 && tx[0] != nil {
		db = tx[0]
	}
	if IsDebug() {
		db = db.Debug()
	}
	return db
}

func Exec(sql string, args ...any) *gorm.DB {
	return DB().Exec(sql, args...)
}

func Raw(sql string, args ...any) *gorm.DB {
	return DB().Raw(sql, args...)
}

func NewAR[T any]() *ActiveRecord[T] {
	return &ActiveRecord[T]{}
}

// Deref returns *p if p is non-nil, otherwise the zero value of T. Intended for
// nullable model columns (`*string`, `*uint32`, …) to keep field accessors as
// one-liners: `return activerecord.Deref(m.Title)`.
func Deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// (including nil misses) for the lifetime of the process.
/* func CacheFindOne[T any](where any, args ...any) *T {
	ar := NewAR[T]()
	return findCached(ar.TableName(), id, func(where any, args ...any) *T {
		u, _ := ar.FindOne(where, args...)
		return u
	})
} */

// FindAllWhere returns every row of T matching the WHERE clause. Uses
// GORM's `.Model(&r)` (not `.Table(name)`) so the schema-parse hits GORM's
// cached statement builder — measurably faster than string-table routing.
//
// Usage — collapses a 6-line hand-written relation method to one line:
//
//	func (m *UserCatalog) GetUserAvailabilitiesModel() ([]*UserAvailability, error) {
//	    return activerecord.FindAllWhere[UserAvailability]("item_id = ?", m.Id)
//	}
func FindAllWhere[T any](where any, args ...any) ([]*T, error) {
	var r []*T
	return r, DB().Model(new(T)).Where(where, args...).Find(&r).Error
}

// FindOneWhere is the singular counterpart to FindAllWhere. Returns nil when
// no row matches (not an error) so callers can `if item == nil` without
// depending on gorm.ErrRecordNotFound.
func FindOneWhere[T any](where any, args ...any) (*T, error) {
	var r T
	err := DB().Model(&r).Where(where, args...).First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// FindAllJoin loads T with an explicit JOIN clause. For M2M-style relations
// that FindAllWhere can't express in a single-table query.
//
// Usage:
//
//	func (m *UserCatalog) GetUsersModel() ([]*UserProfile, error) {
//	    return activerecord.FindAllJoin[UserProfile](
//	        "join `user_catalog_wishlist` on `user_catalog_wishlist`.`user_id` = `user_profile`.`user_id`",
//	        "`catalog_id` = ?", m.Id,
//	    )
//	}
func FindAllJoin[T any](join string, where any, args ...any) ([]*T, error) {
	var r []*T
	return r, DB().Model(new(T)).Joins(join).Where(where, args...).Find(&r).Error
}

func (ar *ActiveRecord[T]) StructName() string {
	return reflect.TypeFor[T]().Name()
}

// TableName returns the DB table name for T. Result is cached forever in
// tableNameCache — hot-path queries no longer pay for a reflect + interface
// assertion each call. First call for a T resolves the name via T's
// TableName() method (when present) or lowercased struct name (fallback).
func (ar *ActiveRecord[T]) TableName() string {
	t := reflect.TypeFor[T]()
	if v, ok := tableNameCache.Load(t); ok {
		return v.(string)
	}
	var model T
	var name string
	if tn, ok := any(&model).(interface{ TableName() string }); ok {
		name = tn.TableName()
	} else {
		name = strings.ToLower(t.Name())
	}
	tableNameCache.Store(t, name)
	return name
}

// Get returns the value of a struct field by name. Pointer fields are
// dereferenced automatically (nil pointer → nil). The name may be either the
// Go struct field name ("Name") or the json tag / column name ("name").
func (ar *ActiveRecord[T]) Get(model *T, field string) any {
	return getField(model, field)
}

// Set assigns value to a struct field by name. Pointer fields are wrapped
// automatically: passing nil clears the pointer, any other value is converted
// to the field's element type and stored via its address. Non-pointer fields
// receive the value directly (converted when necessary). The name may be
// either the Go struct field name or the json tag / column name.
func (ar *ActiveRecord[T]) Set(model *T, field string, value any) error {
	return setField(model, field, value)
}

// Get model (struct) name, e.g. "UserProfile" for T = model.UserProfile.
func (ar *ActiveRecord[T]) GetModel() string {
	var model T
	t := reflect.TypeOf(model)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

// Check existence
func (ar *ActiveRecord[T]) IsExist(query any, args ...any) bool {
	var count int64
	if err := ar.buildQuery(query, args...).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// Count records
func (ar *ActiveRecord[T]) Count(query any, args ...any) (int64, error) {
	var count int64
	if err := ar.buildQuery(query, args...).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Sum field
func (ar *ActiveRecord[T]) Sum(field string, query any, args ...any) (float64, error) {
	var num sql.NullFloat64
	sel := fmt.Sprintf("SUM(%s) AS num", field)
	qb := ar.buildQuery(query, args...).Select(sel)
	if err := qb.Scan(&num).Error; err != nil {
		return 0, err
	}
	if num.Valid {
		return num.Float64, nil
	}
	return 0, nil
}

// Find one
func (ar *ActiveRecord[T]) FindOne(query any, args ...any) (*T, error) {
	var model T
	if err := DB().Where(query, args...).First(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func (ar *ActiveRecord[T]) FindAll(query any, args ...any) ([]*T, error) {
	var models []*T

	if err := DB().Where(query, args...).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

// Find one
func (ar *ActiveRecord[T]) FindOneMapped(query any, args ...any) (map[string]any, error) {
	model, err := ar.FindOne(query, args...)
	if err != nil {
		return nil, err
	}
	return ar.fieldValues(model), nil
}

func (ar *ActiveRecord[T]) FindAllMapped(query any, args ...any) ([]map[string]any, error) {
	models, err := ar.FindAll(query, args...)
	if err != nil {
		return nil, err
	}
	return ar.toArrays(models), nil
}

func (ar *ActiveRecord[T]) SlugifyDB(model *T, field, title string) (string, error) {
	if lib.IsEmpty(title) {
		return "", errors.New(enum.SlugifyEmptyTitle)
	}

	baseSlug := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(title)), " ", "-")
	tbl := ar.TableName()

	// Optional PK exclusion so an UPDATE can keep its own slug.
	pkVals := ar.primaryKeyValue(model)
	var pkClause strings.Builder
	pkArgs := make([]any, 0, len(pkVals))
	for col, val := range pkVals {
		pkClause.WriteString(fmt.Sprintf(" AND `%s` != ?", col))
		pkArgs = append(pkArgs, val)
	}

	// 1. Is the base slug free?
	var baseTaken int64
	baseQuery := fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s` = ?%s",
		tbl, field, pkClause.String())
	baseArgs := append([]any{baseSlug}, pkArgs...)
	if err := DB().Raw(baseQuery, baseArgs...).Scan(&baseTaken).Error; err != nil {
		return "", err
	}
	if baseTaken == 0 {
		return baseSlug, nil
	}

	// 2. Among `<base>-...` rows, find the highest numeric suffix. CAST(... AS
	// UNSIGNED) yields 0 for non-numeric tails (e.g. `<base>-foo`), so they
	// don't displace real numbers in MAX(). CHAR_LENGTH(?)+2 is the 1-indexed
	// position of the character after `<base>-`.
	suffixQuery := fmt.Sprintf(
		"SELECT MAX(CAST(SUBSTRING(`%s`, CHAR_LENGTH(?) + 2) AS UNSIGNED)) FROM `%s` WHERE `%s` LIKE ?%s",
		field, tbl, field, pkClause.String(),
	)
	suffixArgs := append([]any{baseSlug, escapeLikePattern(baseSlug) + "-%"}, pkArgs...)
	var maxSuffix sql.NullInt64
	if err := DB().Raw(suffixQuery, suffixArgs...).Scan(&maxSuffix).Error; err != nil {
		return "", err
	}

	next := int64(1)
	if maxSuffix.Valid && maxSuffix.Int64 > 0 {
		next = maxSuffix.Int64 + 1
	}
	return fmt.Sprintf("%s-%d", baseSlug, next), nil
}

func (ar *ActiveRecord[T]) IsValidField(d1, d2 any) bool {
	return lib.ToString(d1) == lib.ToString(d2)
}

// escapeLikePattern escapes the LIKE metacharacters (`_`, `%`, `\`) so a
// user-derived string is matched literally.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (ar *ActiveRecord[T]) DataQuery(c *action.Context, exclude ...string) *gorm.DB {
	var model T
	fields := ar.fieldTypes(model)
	tbl := ar.TableName()
	stmt := DB().Model(model)
	for name, fieldType := range fields {
		value := c.Query(name)
		if value == "" || lib.InArray(name, exclude) {
			continue
		}
		if fieldType == reflect.TypeFor[string]() {
			stmt = stmt.Where(fmt.Sprintf("`%s`.`%s` LIKE ?", tbl, name), "%"+value+"%")
			continue
		}
		// Non-string columns accept a comma-separated value (e.g. "status=2,3")
		// as an IN(...) filter instead of a single exact match.
		if values := strings.Split(value, ","); len(values) > 1 {
			stmt = stmt.Where(fmt.Sprintf("`%s`.`%s` IN ?", tbl, name), values)
			continue
		}
		stmt = stmt.Where(fmt.Sprintf("`%s`.`%s` = ?", tbl, name), value)
	}
	return stmt
}

func (ar *ActiveRecord[T]) ToArray() map[string]any {
	return ar.fieldTypeNames(new(T))
}

func (ar *ActiveRecord[T]) Find() *gorm.DB {
	return DB().Table(ar.TableName())
}

func (ar *ActiveRecord[T]) Slugify(field string, title string) (slugName string, err error) {
	return ar.SlugifyDB(new(T), field, title)
}

// DeleteAll removes all records matching query, using the default
// connection (autocommit). It's a shortcut for callers with no active
// transaction; anything running inside one should use DeleteAllTx so the
// delete rolls back with the surrounding tx and stays consistent with
// concurrent inserts in the same tx.
func (ar *ActiveRecord[T]) DeleteAll(query any, args ...any) error {
	return ar.DeleteAllTx(nil, query, args...)
}

// DeleteAllTx removes all records matching query inside tx. Pass nil for
// the default connection — matching the behavior of the DB() helper.
// Two variadic parameters aren't allowed in Go, so tx has to lead
// instead of trailing the way SaveDB/Create do it.
func (ar *ActiveRecord[T]) DeleteAllTx(tx *gorm.DB, query any, args ...any) error {
	return DB(tx).Where(query, args...).Delete(new(T)).Error
}

// IsNewRecord returns true when the model has no populated primary key fields.
func (ar *ActiveRecord[T]) IsNewRecord(model *T) bool {
	return len(ar.primaryKeyValue(model)) == 0
}

// Create record
func (ar *ActiveRecord[T]) Create(attributes any, tx ...*gorm.DB) (*T, error) {
	fields, ok := attributes.(map[string]any)
	if !ok {
		return nil, errors.New(enum.CreateAttributesType)
	}
	var model T
	lib.ToLoad(fields, &model)
	if fn, ok := hooks.SaveHooks[reflect.TypeFor[T]()]; ok {
		fn(&model)
	}
	if err := DB(tx...).Create(&model).Error; err != nil {
		return nil, err
	}
	/* if fn, ok := hooks.AfterSave[reflect.TypeFor[T]()]; ok {
		fn(&model)
	} */
	return &model, nil
}

// Save record — marshals fields into a new model instance then persists it.
// Optional: pass a GORM transaction as second argument.
func (ar *ActiveRecord[T]) Save(fields any, tx ...*gorm.DB) (*T, error) {
	attributes, ok := fields.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Save[%s]: fields must be map[string]any, got %T", ar.GetModel(), fields)
	}
	var model T
	if err := unmarshalFields(attributes, &model); err != nil {
		return nil, fmt.Errorf("Save[%s]: %w", ar.GetModel(), err)
	}
	if err := DB(tx...).Save(&model).Error; err != nil {
		return nil, fmt.Errorf("Save[%s]: %w", ar.GetModel(), err)
	}
	return &model, nil
}

// SaveDB upserts model using the provided fields map.
// Optional: pass a GORM transaction as second argument.
func (ar *ActiveRecord[T]) SaveDB(model *T, fields any, tx ...*gorm.DB) (*T, error) {
	if fieldsArray, ok := fields.([]byte); ok {
		if err := unmarshalFields(fieldsArray, &fields); err != nil {
			return nil, fmt.Errorf("SaveDB: %w", err)
		}
	}
	attributes := ar.sanitizeAttributes(fields)
	isNew := ar.IsNewRecord(model)
	if len(attributes) > 0 {
		if err := lib.ToLoad(attributes, &model); err != nil {
			return nil, err
		}
	}
	//	config.Log.Printf("attributes: %v\n", attributes)
	db := DB(tx...)
	if isNew {
		if _, err := ar.insert(model, db); err != nil {
			return nil, err
		}
	} else {
		if _, err := ar.update(model, attributes, db); err != nil {
			return nil, err
		}
	}
	pk := ar.primaryKeyValue(model)
	if len(pk) == 0 {
		return model, nil
	}
	var pkModel T
	if err := db.Where(pk).First(&pkModel).Error; err != nil {
		return nil, err
	}
	return &pkModel, nil
}

// mysqlLockWaitTimeout is the error number MySQL returns for
// "Lock wait timeout exceeded; try restarting transaction".
const mysqlLockWaitTimeout = 1205

// logLockWaitDiagnostics dumps InnoDB's transaction/lock state when a query fails
// with a lock-wait timeout, so the blocking transaction can be identified after
// the fact — the connection holding the lock is otherwise indistinguishable from
// any other idle pooled connection once the wait resolves.
func logLockWaitDiagnostics(err error) {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != mysqlLockWaitTimeout {
		return
	}
	var typ, name, status string
	if row := config.DB.Raw("SHOW ENGINE INNODB STATUS").Row(); row != nil {
		if scanErr := row.Scan(&typ, &name, &status); scanErr == nil {
			config.Log.Printf("lock wait timeout diagnostics:\n%s", status)
			return
		}
	}
}

func (ar *ActiveRecord[T]) insert(model *T, tx *gorm.DB) (*T, error) {
	if fn, ok := hooks.SaveHooks[reflect.TypeFor[T]()]; ok {
		fn(model)
	}
	if err := tx.Create(model).Error; err != nil {
		config.Log.Printf("err: %v\n", err)
		logLockWaitDiagnostics(err)
		return nil, err
	}
	/*
		if fn, ok := hooks.AfterSave[reflect.TypeFor[T]()]; ok {
			fn(model)
		}
	*/
	return model, nil
}

func (ar *ActiveRecord[T]) update(model *T, fields map[string]any, tx ...*gorm.DB) (*T, error) {
	if fn, ok := hooks.UpdateHooks[reflect.TypeFor[T]()]; ok {
		fn(model)
		// Sync updated_at back into the fields map so GORM writes it.
		if f := reflect.ValueOf(model).Elem().FieldByName("UpdatedAt"); f.IsValid() {
			fields["updated_at"] = f.Interface()
		}
	}
	stmt := DB(tx...).Model(model)
	if err := stmt.Updates(fields).Error; err != nil {
		logLockWaitDiagnostics(err)
		return nil, err
	}
	/*
		if fn, ok := hooks.AfterUpdate[reflect.TypeFor[T]()]; ok {
			fn(model)
		}
	*/
	return model, nil
}

func (ar *ActiveRecord[T]) primaryKeyValue(model *T) map[string]any {
	keys := make(map[string]any)
	primaryKeys := ar.primaryKey(model)
	if len(primaryKeys) > 0 {
		for field, key := range primaryKeys {
			val := reflect.ValueOf(model).Elem().FieldByName(key).Interface()
			if lib.NotEmpty(val) {
				keys[field] = val
			}
		}
	}
	return keys
}

// pkSchema returns the primary-key schema (db_column → struct_field_name) for the
// given model type. The result is parsed once and then stored in pkSchemaCache so
// that subsequent calls never allocate a gorm.Statement.
func pkSchema(model any) map[string]string {
	t := reflect.TypeOf(model)
	if v, ok := pkSchemaCache.Load(t); ok {
		return v.(map[string]string)
	}
	stmt := &gorm.Statement{DB: DB()}
	if err := stmt.Parse(model); err != nil {
		return nil
	}
	schema := make(map[string]string, len(stmt.Schema.PrimaryFields))
	for _, field := range stmt.Schema.PrimaryFields {
		schema[field.DBName] = field.Name
	}
	pkSchemaCache.Store(t, schema)
	return schema
}

// unmarshalFields decodes fields into dest via JSON round-trip.
// Accepts map[string]any, []byte (raw JSON), or any json-marshallable value.
func unmarshalFields[T any](fields any, dest *T) error {
	var b []byte
	var err error
	switch v := fields.(type) {
	case []byte:
		b = v
	default:
		if b, err = json.Marshal(v); err != nil {
			return fmt.Errorf("unmarshalFields: marshal: %w", err)
		}
	}
	if err = json.Unmarshal(b, dest); err != nil {
		return fmt.Errorf("unmarshalFields: unmarshal: %w", err)
	}
	return nil
}

func (ar *ActiveRecord[T]) primaryKey(model any) map[string]string {
	schema := pkSchema(model)
	if len(schema) == 0 {
		return nil
	}
	rv := reflect.ValueOf(model).Elem()
	primaryKeys := make(map[string]string, len(schema))
	for dbName, fieldName := range schema {
		if rv.FieldByName(fieldName).IsZero() {
			return nil
		}
		primaryKeys[dbName] = fieldName
	}
	return primaryKeys
}

func (ar *ActiveRecord[T]) toArrays(models []*T) []map[string]any {
	list := make([]map[string]any, 0, len(models))
	for _, model := range models {
		list = append(list, ar.fieldValues(model))
	}
	return list
}

// fieldTypeNames returns a map of JSON tag → type name string for each field.
// Derived from the cached fieldTypes schema so no redundant reflection.
func (ar *ActiveRecord[T]) fieldTypeNames(_ any) map[string]any {
	schema := ar.fieldTypes(nil)
	out := make(map[string]any, len(schema))
	for tag, typ := range schema {
		out[tag] = typ.String()
	}
	return out
}

// fieldValues returns a map of JSON tag → actual value for the given model.
// Pointer fields are dereferenced; nil pointers map to nil. Anonymous/embedded
// fields and json:"-" / untagged fields are skipped, matching buildFieldSchema.
func (ar *ActiveRecord[T]) fieldValues(m any) map[string]any {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return map[string]any{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return map[string]any{}
	}
	t := v.Type()
	out := make(map[string]any, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			continue
		}
		tag, ok := field.Tag.Lookup("json")
		if !ok {
			continue
		}
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}
		if tag == "-" || tag == "" {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				out[tag] = nil
			} else {
				out[tag] = fv.Elem().Interface()
			}
		} else {
			out[tag] = fv.Interface()
		}
	}
	return out
}

func (ar *ActiveRecord[T]) sanitizeAttributes(fields any) map[string]any {
	attribute, ok := fields.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return ar.dirtyAttributes(attribute)
}

func (ar *ActiveRecord[T]) dirtyAttributes(fields map[string]any) map[string]any {
	schema := ar.fieldTypes(nil)
	attributes := make(map[string]any, len(fields))
	for tag := range schema {
		if val, ok := fields[tag]; ok {
			attributes[tag] = val
		}
	}
	return attributes
}

// Build query
func (ar *ActiveRecord[T]) buildQuery(query any, args ...any) *gorm.DB {
	stmt := DB().Model(new(T))
	switch v := query.(type) {
	case string:
		if len(args) > 0 {
			stmt = stmt.Where(v, args...)
		} else {
			stmt = stmt.Where(v)
		}
	case map[string]any:
		stmt = stmt.Where(v)
	case nil:
	default:
		return DB().Model(new(T))
	}
	return stmt
}

// buildFieldSchema parses t's struct fields once and returns a
// json-tag → reflect.Type map.  Tags are normalised (`,omitempty` etc.
// stripped) and sentinel values ("-", "") are skipped.
func buildFieldSchema(t reflect.Type) map[string]reflect.Type {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	schema := make(map[string]reflect.Type)
	if t.Kind() != reflect.Struct {
		return schema
	}
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			continue
		}
		tag, ok := field.Tag.Lookup("json")
		if !ok {
			continue
		}
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}
		if tag == "-" || tag == "" {
			continue
		}
		schema[tag] = field.Type
	}
	return schema
}

// getField reads a struct field by Go name or json tag. Pointer fields are
// dereferenced; nil pointers return nil.
func getField(model any, name string) any {
	fv := lookupField(model, name)
	if !fv.IsValid() {
		return nil
	}
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return nil
		}
		return fv.Elem().Interface()
	}
	return fv.Interface()
}

// setField writes a struct field by Go name or json tag. For pointer fields a
// nil value clears the pointer; any other value is converted to the element
// type and stored via its address. For non-pointer fields the value is
// converted (when necessary) and assigned directly.
func setField(model any, name string, value any) error {
	fv := lookupField(model, name)
	if !fv.IsValid() {
		return fmt.Errorf("activerecord.Set: field %q not found", name)
	}
	if !fv.CanSet() {
		return fmt.Errorf("activerecord.Set: field %q is not settable", name)
	}
	ft := fv.Type()
	if ft.Kind() == reflect.Pointer {
		if value == nil {
			fv.Set(reflect.Zero(ft))
			return nil
		}
		elemType := ft.Elem()
		rv := reflect.ValueOf(value)
		if !rv.Type().AssignableTo(elemType) {
			if !rv.Type().ConvertibleTo(elemType) {
				return fmt.Errorf("activerecord.Set: cannot convert %T to %s", value, elemType)
			}
			rv = rv.Convert(elemType)
		}
		ptr := reflect.New(elemType)
		ptr.Elem().Set(rv)
		fv.Set(ptr)
		return nil
	}
	if value == nil {
		fv.Set(reflect.Zero(ft))
		return nil
	}
	rv := reflect.ValueOf(value)
	if !rv.Type().AssignableTo(ft) {
		if !rv.Type().ConvertibleTo(ft) {
			return fmt.Errorf("activerecord.Set: cannot convert %T to %s", value, ft)
		}
		rv = rv.Convert(ft)
	}
	fv.Set(rv)
	return nil
}

// lookupField resolves name to a settable reflect.Value on model. It matches
// on the Go struct field name first, then falls back to the json tag / column
// name. Returns the zero Value when nothing matches.
func lookupField(model any, name string) reflect.Value {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	if fv := v.FieldByName(name); fv.IsValid() {
		return fv
	}
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			continue
		}
		tag, ok := field.Tag.Lookup("json")
		if !ok {
			continue
		}
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}
		if tag == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// fieldTypes returns a json-tag → reflect.Type map for T, cached per type.
// Fixes: omitempty stripping (#10), json:"-" exclusion (#11), single parse per type (#15).
func (ar *ActiveRecord[T]) fieldTypes(_ any) map[string]reflect.Type {
	t := reflect.TypeFor[T]()
	if v, ok := fieldTypeCache.Load(t); ok {
		return v.(map[string]reflect.Type)
	}
	schema := buildFieldSchema(t)
	fieldTypeCache.Store(t, schema)
	return schema
}

func findCached[T any](kind string, id uint32, load func(uint32) *T) *T {
	if id == 0 {
		return nil
	}
	key := fmt.Sprintf("%s:%d", kind, id)
	if v, ok := reqCache.Load(key); ok {
		// May be a real *T or a negative-cache typed nil. The type
		// assertion handles both paths — a stored (*T)(nil) still asserts
		// successfully to *T with a nil value.
		if t, ok := v.(*T); ok {
			return t
		}
	}
	item := load(id)
	reqCache.Store(key, item)
	return item
}
