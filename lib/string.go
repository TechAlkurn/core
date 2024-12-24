package lib

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gosimple/unidecode"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Separator separator between words
var Separator = "-"

// SeparatorForRe for regexp
var SeparatorForRe = regexp.QuoteMeta(Separator)

// ReInValidChar match invalid slug string
var ReInValidChar = regexp.MustCompile(fmt.Sprintf("[^%sa-zA-Z0-9]", SeparatorForRe))

// ReDupSeparatorChar match duplicate separator string
var ReDupSeparatorChar = regexp.MustCompile(fmt.Sprintf("%s{2,}", SeparatorForRe))

// Version return version
func Version() string {
	return "0.2.0"
}

func Strtr(str string, replace map[string]string) string {
	if len(replace) == 0 || len(str) == 0 {
		return str
	}
	for old, new := range replace {
		str = strings.ReplaceAll(str, old, new)
	}
	return str
}

func FilterContent(text string) string {
	return text
}

func ToCamelCase(str string) string {
	return cases.Title(language.English, cases.Compact).String(str)
}

func ToLower(s string) string {
	return strings.ToLower(s)
}

func Slugify(s string) string {
	s = unidecode.Unidecode(s)
	s = ReInValidChar.ReplaceAllString(s, Separator)
	s = ReDupSeparatorChar.ReplaceAllString(s, Separator)
	s = strings.Trim(s, Separator)
	s = strings.ToLower(s)
	return s
}

func StripTags(content string) string {
	re := regexp.MustCompile(`<(.|\n)*?>`)
	return re.ReplaceAllString(content, "")
}

func ParseID(data []byte) (string, bool) {
	return string(data), false
}
