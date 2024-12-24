package lib

import (
	"fmt"
	"log"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ToString(s any) string {
	str := fmt.Sprintf("%v", s)
	return str
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
	if item, ok := request["form"]; ok {
		form := item.(map[string]any)
		for _, arg := range args {
			for argk, argv := range arg {
				form[argk] = argv
			}
		}
		return ToMarshal(form)
	}
	return nil
}
