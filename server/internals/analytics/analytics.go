package analytics

import "strings"

func SafeEvent(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if len(v) > 60 {
		return v[:60]
	}
	return v
}
