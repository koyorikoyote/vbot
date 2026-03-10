package sanitize

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

var (
	controlCharRe = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
	multiSpaceRe  = regexp.MustCompile(`\s{2,}`)
)

// Text sanitizes user input by removing control characters,
// trimming whitespace, and escaping HTML entities.
func Text(input string) string {
	s := controlCharRe.ReplaceAllString(input, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) || r == '\n' {
			return r
		}
		return -1
	}, s)
	s = multiSpaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = html.EscapeString(s)
	return s
}

// LLMOutput sanitizes LLM-generated text, removing control
// characters. Does not HTML-escape since output is returned
// via JSON API (frontend uses textContent for XSS safety).
func LLMOutput(input string) string {
	s := controlCharRe.ReplaceAllString(input, "")
	s = strings.TrimSpace(s)
	return s
}

// MaxLength truncates a string to maxLen runes.
func MaxLength(input string, maxLen int) string {
	runes := []rune(input)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return input
}
