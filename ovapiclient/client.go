package ovapiclient

import (
	"net/http"
	"time"
)

// HTTPDoer abstracts HTTP requests for testability.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewClient returns an HTTPDoer with sensible timeouts.
func NewClient() HTTPDoer {
	return &http.Client{Timeout: 15 * time.Second}
}

// BuildURL constructs a full URL from base and path segments.
func BuildURL(base string, segments ...string) string {
	u := base
	for _, seg := range segments {
		u += "/" + seg
	}
	return u
}
