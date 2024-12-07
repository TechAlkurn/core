package lib

import (
	"fmt"
	"sort"
	"strings"
)

func ArrayMerge(ss ...[]any) []any {
	n := 0
	for _, v := range ss {
		n += len(v)
	}
	s := make([]any, 0, n)
	for _, v := range ss {
		s = append(s, v...)
	}
	return s
}

func ArrayKeyExists(item string, items map[any]any) bool {
	if _, ok := items[item]; ok {
		return true
	}
	return false
}

func InArray(str string, s []string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func AnyInArray(str any, s []any) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func IntInArray(str int, s []int) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func Uint32InArray(str uint32, s []uint32) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func Current(slice map[any]any, position int) any {
	for i, val := range slice {
		if position == 0 {
			return val
		}
		if i == position {
			return val
		}
	}
	return nil
}

func CurrentMapIntAny(slice map[int]any, position int) any {
	for i, val := range slice {
		if position == 0 {
			return val
		}
		if i == position {
			return val
		}
	}
	return nil
}

func CurrentString(slice []string, position int) string {
	for i, val := range slice {
		if i == position {
			return val
		}
	}
	return ""
}

func Explode(delimiter, text string) []string {
	if len(delimiter) > len(text) {
		return strings.Split(delimiter, text)
	} else {
		return strings.Split(text, delimiter)
	}
}

func Implode(glue string, pieces []string) string {
	return strings.Join(pieces, glue)
}

func ChunkSplit(body string, chunklen uint, end string) string {
	if end == "" {
		end = "\r\n"
	}
	runes, erunes := []rune(body), []rune(end)
	l := uint(len(runes))
	if l <= 1 || l < chunklen {
		return body + end
	}
	ns := make([]rune, 0, len(runes)+len(erunes))
	var i uint
	for i = 0; i < l; i += chunklen {
		if i+chunklen > l {
			ns = append(ns, runes[i:]...)
		} else {
			ns = append(ns, runes[i:i+chunklen]...)
		}
		ns = append(ns, erunes...)
	}
	return string(ns)
}

func Substr(str string, start int, length int) string {
	return str[start : start+length]
}

func ArrayUnique(arr []uint32) []uint32 {
	size := len(arr)
	result := make([]uint32, 0, size)
	temp := map[uint32]struct{}{}
	for i := 0; i < size; i++ {
		if _, ok := temp[arr[i]]; !ok {
			temp[arr[i]] = struct{}{}
			result = append(result, arr[i])
		}
	}
	return result
}

func Intersection(a, b []string) (c []string) {
	m := make(map[string]bool)

	for _, item := range a {
		m[item] = true
	}

	for _, item := range b {
		if _, ok := m[item]; ok {
			c = append(c, item)
		}
	}
	return
}

func IntRemoveItemByIndex(slice []int, index int) []int {
	if index < 0 || index >= len(slice) {
		return slice // Index out of range, return original slice
	}
	return append(slice[:index], slice[index+1:]...)
}

func IntRemoveItemByValue(slice []int, value int) []int {
	result := []int{}
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

func Uint32RemoveItemByIndex(slice []uint32, index int) []uint32 {
	if index < 0 || index >= len(slice) {
		return slice // Index out of range, return original slice
	}
	return append(slice[:index], slice[index+1:]...)
}

func Uint32RemoveItemByValue(slice []uint32, value uint32) []uint32 {
	result := []uint32{}
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

func StringRemoveItemByIndex(slice []string, index int) []string {
	if index < 0 || index >= len(slice) {
		return slice // Index out of range, return original slice
	}
	return append(slice[:index], slice[index+1:]...)
}

func StringRemoveItemByValue(slice []string, value string) []string {
	result := []string{}
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

func ArrayMap(items []any, f func(any) any) []any {
	result := make([]any, len(items))
	for i, item := range items {
		result[i] = f(item)
	}
	return result
}

func ArrayValues(elements map[any]any) []any {
	i, vals := 0, make([]any, len(elements))
	for _, val := range elements {
		vals[i] = val
		i++
	}
	return vals
}

func Ksort(elements map[any]any) map[any]any {
	// Check if all keys are strings
	keys := make([]string, 0, len(elements))
	for key := range elements {
		strKey, ok := key.(string)
		if !ok {
			fmt.Println("All keys must be strings.")
			return nil
		}
		keys = append(keys, strKey)
	}

	// Sort the keys
	sort.Strings(keys)

	// Create a new map with sorted keys
	data := make(map[any]any)
	for _, key := range keys {
		data[key] = elements[key]
	}

	return data
}

func ConvertToUint32Array(anySlice any) ([]uint32, error) {
	var uint32Slice []uint32

	for _, value := range anySlice.([]any) {
		// Type assertion to check if value is uint32
		uint32Slice = append(uint32Slice, ToUint32(ToString(value)))
	}

	return uint32Slice, nil
}

func SelectionNotification(selection string) []uint32 {
	sel := []uint32{}
	if !Empty(selection) {
		selection := strings.Split(selection, ",")
		if len(selection) > 0 {
			for _, item := range selection {
				sel = append(sel, uint32(ToInt(item)))
			}
		}
	}
	return sel
}
