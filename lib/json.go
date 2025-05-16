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

func ToJson(data any) (js map[string]any) {
	j, _ := json.Marshal(data)
	json.Unmarshal(j, &js)
	return js
}
