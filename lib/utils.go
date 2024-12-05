package lib

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func ParseBody(r *http.Request, x interface{}) {
	if body, err := io.ReadAll(r.Body); err == nil {
		if err := json.Unmarshal(body, x); err != nil {
			return
		}
	}
	defer r.Body.Close()
}

func LoadForm(r any, x any) error {
	if Empty(r) {
		return errors.New("invalid json/byte: r")
	}
	j, _ := json.Marshal(r)
	return json.Unmarshal(j, x)
}

func ToLoad(r any, x any) error {
	if Empty(r) {
		return errors.New("invalid json/byte: r")
	}
	j, _ := json.Marshal(r)
	return json.Unmarshal(j, x)
}

func ToMarshal(r any) (j []byte) {
	j, _ = json.Marshal(r)
	return
}

func ToUnmarshal(r []byte, x any) error {
	if Empty(r) {
		return errors.New("invalid json/byte: r")
	}
	return json.Unmarshal(r, x)
}

func MarshalToUnmarshal(r any, x any) error {
	return LoadForm(r, x)
}

// It is applicable for structure
func StructHasField(v interface{}, name string) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	return rv.FieldByName(name).IsValid()
}

// It is applicable for structure
func HasField(v interface{}, name string) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	return rv.FieldByName(name).IsValid()
}

// Function to check if the host is a local host
func IsLocalHost() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	isLocal := false
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			isLocal = true
			break
		}
	}
	return isLocal
}

// GetLocalIP returns the non loopback local IP of the host
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// Helper function to convert string to int
func ConvertToInt(str string) int {
	// You can add error handling here if necessary
	val, _ := strconv.Atoi(str)
	return val
}

func IpAdressPort(c *gin.Context) string {
	host := strings.Split(c.Request.Host, ":")
	return host[1]
}

func SiteUrl(template_url string, params map[string]interface{}) string {
	// var url string
	url := os.Getenv("domainBase")
	if strings.Contains(template_url, "://") {
		url = template_url
	}
	url = url + "/" + template_url
	if len(params) > 0 {
		var query []string
		for key, val := range params {
			query = append(query, fmt.Sprintf("%s=%s", key, val))
		}
		url = url + "?" + strings.Join(query, "&")
	}
	return url
}

func Translate(message string, params map[string]any) string {
	placeholders := make(map[string]string, len(params))
	for name, value := range params {
		placeholders["{"+name+"}"] = fmt.Sprintf("%v", value)
	}

	if len(placeholders) == 0 || placeholders == nil {
		return message
	}
	return Strtr(message, placeholders)
}

func Empty(val interface{}) bool {
	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.String, reflect.Array, reflect.Map, reflect.Slice:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil() || !v.IsValid()
	}
	return reflect.DeepEqual(val, reflect.Zero(v.Type()).Interface())
}

func ExtractValue(model any, field string) (string, error) {
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() != reflect.Ptr {
		return "", errors.New("model is not a pointer")
	}
	if modelValue.Elem().Kind() != reflect.Struct {
		return "", errors.New("pointed value is not a struct")
	}
	FieldName := cases.Title(language.English, cases.Compact).String(field)
	fieldValue := modelValue.Elem().FieldByName(FieldName)
	if !fieldValue.IsValid() {
		return "", errors.New("field not found")
	}
	val := fieldValue.Interface()
	return fmt.Sprintf("%v", val), nil
}

func ReflectValue(fields map[string]any, i any) map[string]interface{} {
	table_fields := map[string]interface{}{}
	modelValue := reflect.ValueOf(i).Elem()
	typeOfT := modelValue.Type()
	for fname, val := range fields {
		for i := 0; i < modelValue.NumField(); i++ {
			name := typeOfT.Field(i).Tag.Get("json")
			if name == fname {
				table_fields[fname] = val
			}
		}
	}
	return table_fields
}

func QueryParams() {
	// Get the current request
	r := &http.Request{}

	// Get the current URL
	url := r.URL

	// Get the query parameters
	query := url.Query()

	// Print the current URL and query parameters
	fmt.Println("Current URL:", url)
	fmt.Println("Query parameters:", query)
}
