package auth

import "strings"

// SanitizedBrowserEnv removes Pippit/XYQ credential variables before spawning
// a browser helper. The login URL carries its own one-time binding and does not
// need any CLI credential from the child process environment.
func SanitizedBrowserEnv(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if !found || isCredentialEnvName(name) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func isCredentialEnvName(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if !strings.HasPrefix(upper, "PIPPIT_") && !strings.HasPrefix(upper, "XYQ_") {
		return false
	}
	if strings.Contains(upper, "ACCESS_KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") {
		return true
	}
	parts := strings.Split(upper, "_")
	for _, part := range parts {
		if part == "AK" {
			return true
		}
	}
	return false
}
