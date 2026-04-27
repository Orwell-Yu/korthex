package redact

import "regexp"

// builtinRules returns the default redaction rules.
// Order matters: bearer_token before api_key to avoid partial overlap.
// All regexes are compiled at call time (once during NewEngine construction).
func builtinRules() []Rule {
	return []Rule{
		{
			Name:        "jwt",
			Pattern:     regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
			Replacement: "[JWT_REDACTED]",
		},
		{
			Name:        "bearer_token",
			Pattern:     regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9_\-.~+/]{20,}=*`),
			Replacement: "${1}[TOKEN_REDACTED]",
		},
		{
			Name:        "api_key",
			Pattern:     regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|password|passwd|authorization)\s*[=:]\s*['"]?)[A-Za-z0-9_\-/.]{16,}`),
			Replacement: "${1}[REDACTED]",
		},
		{
			Name:        "email",
			Pattern:     regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			Replacement: "[EMAIL_REDACTED]",
		},
		{
			Name:        "ipv4",
			Pattern:     regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
			Replacement: "[IP_REDACTED]",
		},
		{
			Name:        "credit_card",
			Pattern:     regexp.MustCompile(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`),
			Replacement: "[CC_REDACTED]",
		},
	}
}
