package config

import (
	"strings"
	"time"

	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/lib"
)

const cacheKey = "system_configs"

func InitConfig() {
	NewConfig().SetConfig()
}

type configuration struct {
	ModelClass string
	Model      any
	Items      map[string]any
	Data       any
}

type Configuration interface {
	GetConfig() (map[string]any, error)
	SetConfig()
	Get(key string) any
	Regenerate()
	ClearCache(key string)
	ClearAllCache()
	GetItems() map[string]any
	Has(key string) bool
	Configs() (map[string]any, error)
	GeneralConfigs(ids []uint32) (map[string]any, error)
	CheckedIsEncryptedKeys() any
}

// NewConfig creates a new Configuration instance
func NewConfig() Configuration {
	return &configuration{
		Items: make(map[string]any),
	}
}

// configQuery fetches all enabled config fields plus their current values and
// available options in a single joined query. Only columns actually consumed by
// resolveValue are selected.
const configQuery = `
SELECT
    c.name        AS field_name,
    c.type        AS input_type,
    c.is_encrypted,
    GROUP_CONCAT(
        DISTINCT cv.value
        ORDER BY cv.id
        SEPARATOR '||'
    ) AS current_values,
    GROUP_CONCAT(
        DISTINCT CONCAT(co.option_code, ':::', COALESCE(co.option_value, ''))
        ORDER BY co.sort_order, co.option_id
        SEPARATOR '||'
    ) AS available_options
FROM core_config c
LEFT JOIN core_config_value cv
    ON cv.field_id = c.id
LEFT JOIN core_config_option co
    ON co.field_id = c.id
WHERE c.status = ?`

const configGroupOrder = `
GROUP BY c.id, c.name, c.type, c.is_encrypted
ORDER BY c.order_by, c.id`

// configFlatRow is the raw DB scan result
type configFlatRow struct {
	FieldName        string  `gorm:"column:field_name"`
	InputType        string  `gorm:"column:input_type"`
	IsEncrypted      *uint8  `gorm:"column:is_encrypted"`
	CurrentValues    *string `gorm:"column:current_values"`
	AvailableOptions *string `gorm:"column:available_options"`
}

// GeneralConfigs fetches config values for the given field IDs in a single query.
func (c *configuration) GeneralConfigs(ids []uint32) (map[string]any, error) {
	if len(ids) == 0 {
		return map[string]any{}, nil
	}
	var rows []configFlatRow
	err := DB.Raw(
		configQuery+` AND c.id IN ?`+configGroupOrder,
		cons.CORE_CONFIG_STATUS_YES, ids,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return c.mapRows(rows), nil
}

// Configs fetches all enabled config values in a single query.
func (c *configuration) Configs() (map[string]any, error) {
	var rows []configFlatRow
	err := DB.Raw(configQuery+configGroupOrder, cons.CORE_CONFIG_STATUS_YES).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return c.mapRows(rows), nil
}

// mapRows turns flat DB rows into map[field_name]resolved_value.
func (c *configuration) mapRows(rows []configFlatRow) map[string]any {
	result := make(map[string]any, len(rows))
	for _, row := range rows {
		result[row.FieldName] = c.resolveValue(row)
	}
	return result
}

func (c *configuration) SetConfig() {
	if configs, found := Lcache.Get(cacheKey); found {
		c.Items = configs.(map[string]any)
		return
	}
	configs, err := c.Configs()
	if err != nil {
		panic(err)
	}
	Lcache.Set(cacheKey, configs, 24*time.Hour)
	c.Items = configs
}

func (c *configuration) GetConfig() (map[string]any, error) {
	return c.Items, nil
}

// resolveValue mirrors the original _item() logic but works purely from the
// already-fetched flat row — zero extra DB calls.
func (c *configuration) resolveValue(row configFlatRow) any {
	options := parseOptions(row.AvailableOptions)
	values := parseValues(row.CurrentValues)
	if len(values) == 0 {
		return ""
	}
	switch row.InputType {
	case "text", "textarea":
		val := values[0]
		if row.IsEncrypted != nil && *row.IsEncrypted == cons.CORE_CONFIG_IS_ENCRYPTED_YES {
			return decryptValue(val)
		}
		return val

	case "radio":
		val := values[0]
		if lib.IsEmpty(val) {
			return nil
		}
		if _, ok := options[val]; !ok {
			return nil
		}
		return val

	default:
		// checkbox / multi-select — return slice of matched option maps
		result := make([]any, 0, len(values))
		for _, val := range values {
			if opt, ok := options[val]; ok {
				result = append(result, map[string]any{"option_code": val, "option_value": opt})
			}
		}
		return result
	}
}

func (c *configuration) Get(key string) any {
	if len(c.Items) == 0 {
		c.SetConfig()
	}
	return c.Items[key]
}

func (c *configuration) Regenerate() {
	Lcache.Delete(cacheKey)
	c.SetConfig()
}

func (c *configuration) GetItems() map[string]any {
	if len(c.Items) == 0 {
		c.SetConfig()
	}
	return c.Items
}

func (c *configuration) Has(key string) bool {
	_, exists := c.Items[key]
	return exists
}

func (c *configuration) ClearCache(key string) {
	if key == "" {
		key = cacheKey
	}
	Lcache.Delete(key)
}

func (c *configuration) ClearAllCache() {
	Lcache.Flush()
}

func (c *configuration) CheckedIsEncryptedKeys() any {
	if rs := c.GetItems(); lib.NotEmpty(rs) && len(rs) > 0 {
		var keys []string
		for key := range rs {
			keys = append(keys, key)
		}
		var allowedKeys []string
		db := DB.Raw("SELECT `name` FROM `core_config` WHERE (`is_encrypted` = 0 OR `is_encrypted` IS NULL) AND `name` IN (?)", keys).Scan(&allowedKeys)
		if err := db.Error; err != nil {
			return err
		}
		rsFinal := make(map[string]any)
		for _, key := range allowedKeys {
			rsFinal[key] = rs[key]
		}
		return rsFinal
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// parseOptions splits "code:::value||code:::value" into map[code]value
func parseOptions(raw *string) map[string]string {
	out := map[string]string{}
	if raw == nil || *raw == "" {
		return out
	}
	for entry := range strings.SplitSeq(*raw, "||") {
		parts := strings.SplitN(entry, ":::", 2)
		if len(parts) == 2 {
			out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return out
}

// parseValues splits "val1||val2" into []string
func parseValues(raw *string) []string {
	if raw == nil || *raw == "" {
		return nil
	}
	var out []string
	for v := range strings.SplitSeq(*raw, "||") {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// decryptValue handles encrypted field decryption inline
func decryptValue(value string) any {
	if strings.Contains(value, "::") {
		return lib.SecurityDecrypt(value)
	}
	return value
}

var Get = func(key string) any {
	return NewConfig().Get(key)
}

var GetString = func(key string) string {
	return lib.ToString(Get(key))
}

var GetUint = func(key string) uint {
	return lib.ToUint(Get(key))
}

var GetInt = func(key string) int {
	return lib.ToInt(Get(key))
}

var GetUint32 = func(key string) uint32 {
	return lib.ToUint32(Get(key))
}

var GetFloat64 = func(key string) float64 {
	return lib.ToFloat64(Get(key))
}
