package hooks

import (
	"reflect"
	"time"
)

// SaveHooks maps a model type to its BEFORE-save (i.e. before INSERT) hook.
// Populated by helpers.RegisterHookSave in each helper's init().
var SaveHooks = map[reflect.Type]func(any){}

// UpdateHooks maps a model type to its BEFORE-update hook.
// Populated by helpers.RegisterHookUpdate in each helper's init().
var UpdateHooks = map[reflect.Type]func(any){}

// AfterSaveHooks maps a model type to its AFTER-save (i.e. after INSERT) hook.
// Populated by helpers.RegisterHookAfterSave in each helper's init().
var AfterSaveHooks = map[reflect.Type]func(any){}

// AfterUpdateHooks maps a model type to its AFTER-update hook.
// Populated by helpers.RegisterHookAfterUpdate in each helper's init().
var AfterUpdateHooks = map[reflect.Type]func(any){}

// RegisterHookSave registers a before-insert hook for model type T.
// Call from init() in each model's helper file.
func RegisterHookSave[T any](fn func(*T)) {
	SaveHooks[reflect.TypeFor[T]()] = func(m any) { fn(m.(*T)) }
}

// RegisterHookUpdate registers a before-update hook for model type T.
func RegisterHookUpdate[T any](fn func(*T)) {
	UpdateHooks[reflect.TypeFor[T]()] = func(m any) { fn(m.(*T)) }
}

// RegisterHookAfterSave registers an after-insert hook for model type T.
func RegisterHookAfterSave[T any](fn func(*T)) {
	AfterSaveHooks[reflect.TypeFor[T]()] = func(m any) { fn(m.(*T)) }
}

// RegisterHookAfterUpdate registers an after-update hook for model type T.
func RegisterHookAfterUpdate[T any](fn func(*T)) {
	AfterUpdateHooks[reflect.TypeFor[T]()] = func(m any) { fn(m.(*T)) }
}

// RegisterTimestampHooks registers standard CreatedAt/UpdatedAt hooks for
// model type T. Most entities only need this — no custom hook logic required.
//
// Before — 20+ lines per helper file:
//
//	func init() {
//	    hooks.RegisterHookSave(func(v *model.UserAward) {
//	        NewUserAward(v).OnSave()
//	    })
//	    hooks.RegisterHookUpdate(func(v *model.UserAward) {
//	        NewUserAward(v).OnUpdate()
//	    })
//	}
//	func (entity *UserAward) OnSave()   { entity.CreatedAt = uint32(time.Now().Unix()); entity.UpdatedAt = entity.CreatedAt }
//	func (entity *UserAward) OnUpdate() { entity.UpdatedAt = uint32(time.Now().Unix()) }
//
// After — one line per helper init():
//
//	func init() { hooks.RegisterTimestampHooks[model.UserAward]() }
func RegisterTimestampHooks[T any]() {
	RegisterHookSave(func(v *T) {
		now := uint32(time.Now().Unix())
		val := reflect.ValueOf(v).Elem()
		setTimestamp(val.FieldByName("CreatedAt"), now)
		setTimestamp(val.FieldByName("UpdatedAt"), now)
	})
	RegisterHookUpdate(func(v *T) {
		now := uint32(time.Now().Unix())
		val := reflect.ValueOf(v).Elem()
		setTimestamp(val.FieldByName("UpdatedAt"), now)
	})
}

// setTimestamp writes now into f. Handles both uint-family fields (SetUint
// directly) and pointer-to-uint fields (allocates a new value + assigns the
// pointer). Silently skips fields of any other type/kind or unsettable ones.
func setTimestamp(f reflect.Value, now uint32) {
	if !f.IsValid() || !f.CanSet() {
		return
	}
	if f.Kind() == reflect.Pointer {
		elemType := f.Type().Elem()
		switch elemType.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			p := reflect.New(elemType)
			p.Elem().SetUint(uint64(now))
			f.Set(p)
		}
		return
	}
	if f.CanUint() {
		f.SetUint(uint64(now))
	}
}

// ModelHooks is implemented by any model whose pointer type provides the full
// set of lifecycle callbacks. Method signatures match the helper convention
// (e.g. *helpers.User, *helpers.CoreMediaItem): each returns an error so
// callers can short-circuit on validation failure.
type ModelHooks interface {
	OnSave() error
	OnUpdate() error
}

// RegisterHooks wires up the before-save and before-update hooks (OnSave /
// OnUpdate) for any model whose pointer type implements ModelHooks. One line
// in the helper's init() replaces RegisterHookSave + RegisterHookUpdate.
// After-hooks are not covered; use RegisterHookAfterSave / AfterUpdate.
//
// Usage: func init() { hooks.RegisterHooks[model.User]() }
//
// Panics at init time if *T does not implement ModelHooks.
func RegisterHooks[T any]() {
	var zero T
	if _, ok := any(&zero).(ModelHooks); !ok {
		name := reflect.TypeFor[T]().Name()
		if name == "" {
			name = reflect.TypeFor[T]().String()
		}
		panic("hooks.RegisterHooks: *" + name + " does not implement ModelHooks")
	}
	t := reflect.TypeFor[T]()
	SaveHooks[t] = func(m any) { _ = m.(ModelHooks).OnSave() }
	UpdateHooks[t] = func(m any) { _ = m.(ModelHooks).OnUpdate() }
	// AfterSaveHooks[t] = func(m any) { _ = m.(ModelHooks).AfterSave() }
	// AfterUpdateHooks[t] = func(m any) { _ = m.(ModelHooks).AfterUpdate() }
}
