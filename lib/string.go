package lib

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

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

func HTMLToText2(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	skip := false

	blockTags := map[string]bool{
		"p": true, "div": true, "h1": true, "h2": true, "h3": true,
		"h4": true, "h5": true, "h6": true, "ul": true, "ol": true,
		"li": true, "br": true, "hr": true, "table": true, "tr": true,
		"td": true, "th": true, "pre": true, "blockquote": true,
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "script" || n.Data == "style" {
				skip = true
				return
			}
			if blockTags[n.Data] {
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
			}
			if n.Data == "br" || n.Data == "hr" {
				buf.WriteByte('\n')
			}
		}

		if !skip {
			if n.Type == html.TextNode {
				text := strings.TrimSpace(n.Data)
				if text != "" {
					buf.WriteString(text)
					buf.WriteByte(' ')
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}

		if n.Type == html.ElementNode {
			if n.Data == "script" || n.Data == "style" {
				skip = false
				return
			}
			if blockTags[n.Data] {
				buf.WriteByte('\n')
			}
		}
	}

	f(doc)

	// Post-process the text to collapse whitespace
	text := buf.String()

	// Collapse all whitespace sequences to a single space
	text = strings.Join(strings.Fields(text), " ")

	// Collapse multiple newlines to a single newline
	re := regexp.MustCompile(`\n+`)
	text = re.ReplaceAllString(text, "\n")

	// Trim leading and trailing whitespace
	text = strings.TrimSpace(text)

	return text, nil
}

func UTF8Decode(s string) string {
	var builder strings.Builder
	builder.Grow(len(s)) // Pre-allocate for efficiency
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 sequence (1 byte)
			builder.WriteByte('?')
			i += size
		} else {
			if r <= 0xFF { // Check if rune is within ISO-8859-1 range
				builder.WriteByte(byte(r))
			} else {
				builder.WriteByte('?') // Character not representable in ISO-8859-1
			}
			i += size
		}
	}
	return builder.String()
}

// UTF8Encode converts an ISO-8859-1 (Latin-1) encoded string to UTF-8
func UTF8Encode(s string) string {
	var builder strings.Builder
	builder.Grow(len(s) * 2) // Pre-allocate for worst-case expansion (each char → 2 bytes)
	for i := range len(s) {
		// Convert each ISO-8859-1 byte directly to a Unicode code point
		// (ISO-8859-1 maps 1:1 to first 256 Unicode code points)
		builder.WriteRune(rune(s[i]))
	}
	return builder.String()
}

func ExtractLink(content string) []string {
	// Regex for URLs (http/https/ftp/mailto)
	urlRegex := `(https?|ftp|file|mailto):\/\/[^\s"'<>]+`
	re := regexp.MustCompile(urlRegex)
	return re.FindAllString(content, -1)
}
