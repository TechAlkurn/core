package lib

import (
	"reflect"
	"strings"
)

func PrimaryKey(model any) map[string]string {
	primaryKey := make(map[string]string) // Initialize the map
	valueOf := reflect.ValueOf(model)
	if valueOf.Kind() == reflect.Ptr {
		valueOf = valueOf.Elem()
	}

	if valueOf.Kind() == reflect.Struct {
		for i := 0; i < valueOf.NumField(); i++ {
			field := valueOf.Type().Field(i)

			if tag, ok := field.Tag.Lookup("gorm"); ok {
				if strings.Contains(tag, "primaryKey") {
					if tagKey, ok := field.Tag.Lookup("json"); ok {
						primaryKey[tagKey] = field.Name
					}
				}
			}
		}
	}
	return primaryKey
}

func Attributes(model any) map[string]any {
	fieldsKey := make(map[string]any) // Initialize the map
	valueOf := reflect.ValueOf(model)
	if valueOf.Kind() == reflect.Ptr {
		valueOf = valueOf.Elem()
	}
	if valueOf.Kind() == reflect.Struct {
		for i := 0; i < valueOf.NumField(); i++ {
			field := valueOf.Type().Field(i)
			if tagKey, ok := field.Tag.Lookup("json"); ok {
				fieldsKey[tagKey] = field.Name
			}
		}
	}
	return fieldsKey
}

func AttributeType(model any) map[string]any {
	fieldsKey := make(map[string]any) // Initialize the map
	valueOf := reflect.ValueOf(model)
	if valueOf.Kind() == reflect.Ptr {
		valueOf = valueOf.Elem()
	}
	if valueOf.Kind() == reflect.Struct {
		for i := 0; i < valueOf.NumField(); i++ {
			field := valueOf.Type().Field(i)
			if tagKey, ok := field.Tag.Lookup("json"); ok {
				fieldsKey[tagKey] = field.Type
			}
		}
	}
	return fieldsKey
}

func DirtyAttributes(model any, attributes map[string]any) map[string]any {
	modelAttributes := Attributes(model)
	keyAttributes := []string{}
	for key := range modelAttributes {
		keyAttributes = append(keyAttributes, key)
	}

	mapAttributes := make(map[string]any)
	for key := range attributes {
		if InArray(key, keyAttributes) {
			mapAttributes[key] = attributes[key]
		}
	}
	return mapAttributes
}

func ValidationAuthclient(source, sourceId string) bool {
	return true
}

func Pluck(slice interface{}, fieldName string) []interface{} {
	// Ensure the input is a slice
	sliceValue := reflect.ValueOf(slice)
	if sliceValue.Kind() != reflect.Slice {
		panic("Pluck: first argument must be a slice")
	}
	// Prepare a result slice
	result := make([]interface{}, 0, sliceValue.Len())
	// Iterate over the slice
	for i := 0; i < sliceValue.Len(); i++ {
		// Get the element of the slice
		item := sliceValue.Index(i)
		// Ensure the element is a struct or a pointer to a struct
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			panic("Pluck: slice elements must be structs or pointers to structs")
		}

		// Extract the field value
		fieldValue := item.FieldByName(fieldName)
		if !fieldValue.IsValid() {
			panic("Pluck: field '" + fieldName + "' does not exist in struct")
		}
		// Append the field value to the result
		result = append(result, fieldValue.Interface())
	}

	return result
}
