package activerecord

import "github.com/TechAlkurn/core/lib"

// Mapper gives typed, panic-free reads over a decoded JSON request body
// (Payload.GetForm()). Value receivers, so a call can chain straight off
// ToMapped: activerecord.ToMapped(in.Form).ToString("title").
type Mapper struct {
	data map[string]any
}

// ToMapped decodes a JSON body into a Mapper. Invalid or empty JSON yields an
// empty Mapper — every read then returns its zero value.
func ToMapped(data []byte) Mapper {
	return Mapper{data: lib.ToMapped(data)}
}

// Data returns the underlying map (nil when the body was empty or invalid).
func (m Mapper) Data() map[string]any {
	return m.data
}

// Has reports whether key was sent at all, even with an empty value.
func (m Mapper) Has(key string) bool {
	_, ok := m.data[key]
	return ok
}

// Lookup returns the raw value for key and whether it was sent — the
// comma-ok form of Raw.
func (m Mapper) Lookup(key string) (any, bool) {
	val, ok := m.data[key]
	return val, ok
}

// Set stores val under key. The map is shared with BaseForm.Form, so the
// write is visible through both. No-op on a Mapper with no data (an invalid
// body decoded by ToMapped); BaseForm always has a non-nil map.
func (m Mapper) Set(key string, val any) {
	if m.data == nil {
		return
	}
	m.data[key] = val
}

// Raw returns the value for key exactly as decoded (nil when missing), with
// no empty-value filtering — for callers that need the original type.
func (m Mapper) Raw(key string) any {
	return m.data[key]
}

// Get returns the value for key, or nil when it is missing or empty.
func (m Mapper) Get(key string) any {
	if val, ok := m.data[key]; ok && lib.NotEmpty(val) {
		return val
	}
	return nil
}

// ToString returns key as a string ("" when missing or empty).
func (m Mapper) ToString(key string) string {
	return lib.ToString(m.Get(key))
}

// ToFloat64 returns key as a float64 (0 when missing, empty or not numeric).
func (m Mapper) ToFloat64(key string) float64 {
	return lib.ToFloat64(m.Get(key))
}

// ToInt returns key as an int (0 when missing, empty or not numeric).
func (m Mapper) ToInt(key string) int {
	return lib.ToInt(m.Get(key))
}

// ToUint returns key as a uint (0 when missing, empty or not numeric).
func (m Mapper) ToUint(key string) uint {
	return lib.ToUint(m.Get(key))
}

// ToUint32 returns key as a uint32 — the type of most ids in this codebase.
func (m Mapper) ToUint32(key string) uint32 {
	return lib.ToUint32(m.Get(key))
}

// ToMap returns key as a nested object, or nil when it is missing or not an
// object. An empty object `{}` is returned as-is (non-nil).
func (m Mapper) ToMap(key string) map[string]any {
	v, _ := m.data[key].(map[string]any)
	return v
}

// ToSlice returns key as a JSON array, or nil when it is missing or not an
// array.
func (m Mapper) ToSlice(key string) []any {
	v, _ := m.data[key].([]any)
	return v
}

// ToList returns key as a list of strings. A missing/empty key yields nil, a
// single value yields a one-item list, and empty items are dropped.
func (m Mapper) ToList(key string) (data []string) {
	switch v := m.Get(key).(type) {
	case []any:
		for _, item := range v {
			if s := lib.ToString(item); s != "" {
				data = append(data, s)
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				data = append(data, s)
			}
		}
	case nil:
	default:
		if s := lib.ToString(v); s != "" {
			data = append(data, s)
		}
	}
	return data
}
