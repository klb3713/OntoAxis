package runtime

import (
	"regexp"
	"strings"
)

// maxRedactedLength caps one persisted/emitted runtime error line.
const maxRedactedLength = 300

var (
	// urlCredentialsPattern strips userinfo from URLs such as
	// postgres://user:password@host/db. The scheme is kept for diagnostics.
	urlCredentialsPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://)[^/\s@]+@`)
	// secretAssignmentPattern strips values assigned to common credential names,
	// including JSON-style `"token": "..."` fragments.
	secretAssignmentPattern = regexp.MustCompile(`(?i)(["']?[a-z0-9_.\-]*(?:password|passwd|secret|token|api[_-]?key|authorization|credential|access[_-]?key|private[_-]?key)[a-z0-9_.\-]*["']?\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}\]]+)`)
)

// Redact trims a runtime error to a single bounded line and removes common
// credential shapes before the message is persisted, logged or returned. It is
// deliberately best-effort: never rely on it as a security boundary.
func Redact(message string) string {
	if message == "" {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
	line = urlCredentialsPattern.ReplaceAllString(line, "$1***@")
	line = secretAssignmentPattern.ReplaceAllString(line, "$1***")
	line = strings.Join(strings.Fields(line), " ")
	if line == "" {
		return ""
	}
	runes := []rune(line)
	if len(runes) > maxRedactedLength {
		line = string(runes[:maxRedactedLength]) + "…"
	}
	return line
}
