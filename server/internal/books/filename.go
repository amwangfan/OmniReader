package books

import (
	"regexp"
	"strings"
	"time"
)

const DefaultFilenameTemplate = "{{Book}}-{{Author}}.epub"

var invalidFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func RenderFilenameTemplate(pattern string, title string, author string, now time.Time) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = DefaultFilenameTemplate
	}
	if pattern == DefaultFilenameTemplate && strings.TrimSpace(author) == "" {
		pattern = "{{Book}}.epub"
	}

	replacements := map[string]string{
		"{{Book}}":     sanitizeFilenamePart(title),
		"{{Author}}":   sanitizeFilenamePart(author),
		"{{YYMMDD}}":   now.Format("060102"),
		"{{YYYYMMDD}}": now.Format("20060102"),
	}
	filename := pattern
	for token, value := range replacements {
		filename = strings.ReplaceAll(filename, token, value)
	}
	filename = strings.TrimSpace(invalidFilenameChars.ReplaceAllString(filename, "_"))
	filename = strings.Trim(filename, " .-_")
	if filename == "" {
		filename = "book"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".epub") {
		filename += ".epub"
	}
	return filename
}

func sanitizeFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	value = invalidFilenameChars.ReplaceAllString(value, "_")
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Trim(value, " .-_")
	if value == "" {
		return "Unknown"
	}
	return value
}
