package seabird_webhook_receiver

import (
	"net/url"
	"strings"
)

// escapePath escapes each segment of a slash-separated path.
func escapePath(p string) string {
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// isZeroSHA reports whether id is an all-zero commit ID, which forges use
// for the missing side of a created or deleted ref.
func isZeroSHA(id string) bool {
	return id != "" && strings.Trim(id, "0") == ""
}
