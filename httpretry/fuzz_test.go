package httpretry

import (
	"net/http"
	"testing"
	"time"
)

func FuzzRetryAfterDelay(f *testing.F) {
	f.Add("120")
	f.Add("Wed, 21 Oct 2037 07:28:00 GMT")
	f.Add("not-a-delay")
	f.Fuzz(func(t *testing.T, value string) {
		response := &http.Response{Header: http.Header{"Retry-After": []string{value}}}
		delay := retryAfterDelay(response, time.Unix(0, 0))
		if delay < 0 {
			t.Fatalf("Retry-After %q produced negative delay %v", value, delay)
		}
	})
}
