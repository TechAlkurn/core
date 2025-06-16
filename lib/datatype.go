package lib

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ToString(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

func ToUint32(s any) uint32 {
	str := fmt.Sprintf("%v", s)
	num, _ := strconv.ParseUint(str, 10, 32)
	return uint32(num)
}

func ToUint64(s any) uint64 {
	str := fmt.Sprintf("%v", s)
	num, _ := strconv.ParseUint(str, 10, 64)
	return uint64(num)
}

func ToUint(s any) uint {
	str := fmt.Sprintf("%v", s)
	num, _ := strconv.ParseUint(str, 10, 64)
	return uint(num)
}

func ToInt32(s any) int32 {
	str := fmt.Sprintf("%v", s)
	num, _ := strconv.ParseInt(str, 10, 32)
	return int32(num)
}

func ToInt64(s any) int64 {
	str := fmt.Sprintf("%v", s)
	num, _ := strconv.ParseInt(str, 10, 64)
	return int64(num)
}

func ToInt(s any) int {
	str := fmt.Sprintf("%v", s)
	num, _ := strconv.ParseInt(str, 10, 64)
	return int(num)
}

func ToFloat64(s any) float64 {
	str := fmt.Sprintf("%v", s)
	num, _ := strconv.ParseFloat(str, 64)
	return float64(num)
}

func IsNumeric(s any) bool {
	str := fmt.Sprintf("%v", s)
	return regexp.MustCompile(`\d`).MatchString(str)
}

func ToBool(s any) bool {
	str := fmt.Sprintf("%v", s)
	b, err := strconv.ParseBool(str)
	if err != nil {
		log.Fatal(err)
	}
	return b
}

func BindJSON(c *gin.Context) (form map[string]any, err error) {
	var request map[string]any
	if err = c.ShouldBindJSON(&request); err != nil {
		return nil, err
	}
	if item, ok := request["form"]; ok {
		return item.(map[string]any), nil
	}
	return nil, nil
}

func ShouldBindJSON(c *gin.Context, args ...map[string]any) (j []byte) {
	if !InArray(c.Request.Method, []string{"POST", "PUT", "PATCH"}) {
		return nil
	}
	var request map[string]any
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil
	}
	form, ok := request["form"].(map[string]any)
	if !ok {
		return nil
	}
	// Merge additional arguments into the "form" map
	for _, arg := range args {
		for key, value := range arg {
			form[key] = value
		}
	}
	// Marshal the modified "form" map into JSON
	data, err := json.Marshal(form)
	if err != nil {
		return nil
	}
	return data
}
