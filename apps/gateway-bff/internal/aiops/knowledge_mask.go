package aiops

import "regexp"

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|api[_-]?key)(\s*[:=]\s*)([^\s,;]+)`),
	regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._\-]+`),
	regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`),
}

func maskSensitiveText(value string) string {
	result := value
	for index, pattern := range sensitivePatterns {
		if index == 0 {
			result = pattern.ReplaceAllString(result, `$1$2***`)
			continue
		}
		if index == 1 {
			result = pattern.ReplaceAllString(result, `$1***`)
			continue
		}
		result = pattern.ReplaceAllString(result, "[REDACTED PRIVATE KEY]")
	}
	return result
}
