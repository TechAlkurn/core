package lib

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/gosimple/unidecode"
	"golang.org/x/net/html"
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

// Namify formats a given string into a clean, lowercase, underscore-separated name.
func Namify(s string) string {
	// Trim spaces from start and end
	s = Slugify(s)
	s = regexp.MustCompile(`[^a-z0-9\s]+`).ReplaceAllString(s, "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
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

func TruncateWord(s string, max int) string {
	if max > len(s) {
		return s
	}
	return s[:strings.LastIndexAny(s[:max], " .,:;-")]
}

func HTMLToText(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && (n.Data == "br" || n.Data == "p" || n.Data == "div" || n.Data == "li") {
			buf.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return html.UnescapeString(strings.TrimSpace(buf.String()))
}
