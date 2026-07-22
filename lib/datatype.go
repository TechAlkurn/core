package lib

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
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

func ShouldBindJSON(c *gin.Context, args ...map[string]any) (j []byte, err error) {
	if !InArray(c.Request.Method, []string{"POST", "PUT", "PATCH"}) {
		return nil
	}
	var request map[string]any
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, err
	}
	form, ok := request["form"].(map[string]any)
	if !ok {
		return nil, errors.New("invalid form data")
	}
	// Merge additional arguments into the "form" map
	for _, arg := range args {
		maps.Copy(form, arg)
	}
	// Marshal the modified "form" map into JSON
	return json.Marshal(form)
}

// String returns a pointer to the string value passed in.
func String(v string) *string {
	return &v
}

// String returns a pointer to the string value passed in.
func Int(v int) *int {
	return &v
}

// String returns a pointer to the string value passed in.
func Int8(v int8) *int8 {
	return &v
}

// String returns a pointer to the string value passed in.
func Int16(v int16) *int16 {
	return &v
}

// String returns a pointer to the string value passed in.
func Int32(v int32) *int32 {
	return &v
}

// String returns a pointer to the string value passed in.
func Int64(v int64) *int64 {
	return &v
}

// String returns a pointer to the string value passed in.
func Bool(v bool) *bool {
	return &v
}

// String returns a pointer to the string value passed in.
func Float32(v float32) *float32 {
	return &v
}

// String returns a pointer to the string value passed in.
func Float64(v float64) *float64 {
	return &v
}

// String returns a pointer to the string value passed in.
func Uint(v uint) *uint {
	return &v
}

// String returns a pointer to the string value passed in.
func Uint32(v uint32) *uint32 {
	return &v
}

// String returns a pointer to the string value passed in.
func Uint64(v uint64) *uint64 {
	return &v
}

// String returns a pointer to the string value passed in.
func Uint8(v uint8) *uint8 {
	return &v
}

// String returns a pointer to the string value passed in.
func Uint16(v uint16) *uint16 {
	return &v
}

// String returns a pointer to the string value passed in.
func Uintptr(v uintptr) *uintptr {
	return &v
}

// String returns a pointer to the string value passed in.
func Byte(v byte) *byte {
	return &v
}

// String returns a pointer to the string value passed in.
func Rune(v rune) *rune {
	return &v
}

// String returns a pointer to the string value passed in.
func Complex64(v complex64) *complex64 {
	return &v
}

// String returns a pointer to the string value passed in.
func Complex128(v complex128) *complex128 {
	return &v
}

// String returns a pointer to the string value passed in.
func Slice(v []string) *[]string {
	return &v
}

// String returns a pointer to the string value passed in.
func Map(v map[string]string) *map[string]string {
	return &v
}

// String returns a pointer to the string value passed in.
func Chan(v chan string) *chan string {
	return &v
}

// String returns a pointer to the string value passed in.
func Interface(v interface{}) *interface{} {
	return &v
}

// String returns a pointer to the string value passed in.
func Ptr(v *string) *string {
	return v
}

// String returns a pointer to the string value passed in.
func Any(v any) *any {
	return &v
}

// String returns a pointer to the string value passed in.
func StringSlice(v []string) *[]string {
	return &v
}

// String returns a pointer to the string value passed in.
func StringMap(v map[string]string) *map[string]string {
	return &v
}

// String returns a pointer to the string value passed in.
func StringChan(v chan string) *chan string {
	return &v
}

// String returns a pointer to the string value passed in.
func StringInterface(v interface{}) *interface{} {
	return &v
}

// String returns a pointer to the string value passed in.
func StringPtr(v *string) *string {
	return v
}

// String returns a pointer to the string value passed in.
func StringAny(v any) *any {
	return &v
}

// String returns a pointer to the string value passed in.
func IntSlice(v []int) *[]int {
	return &v
}

// String returns a pointer to the string value passed in.
func IntMap(v map[string]int) *map[string]int {
	return &v
}

// String returns a pointer to the string value passed in.
func IntChan(v chan int) *chan int {
	return &v
}

// String returns a pointer to the string value passed in.
func IntInterface(v interface{}) *interface{} {
	return &v
}
