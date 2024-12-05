package lib

import "encoding/json"

func JsonEncode(data any) (string, error) {
	jsons, err := json.Marshal(data)
	return string(jsons), err
}

func JsonDecode(data string) (map[string]any, error) {
	var d map[string]any
	err := json.Unmarshal([]byte(data), &d)
	return d, err
}
