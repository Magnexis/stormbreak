package httpretry

import (
	"net/http"
	"strings"
	"testing"
)

func TestStatusError(t *testing.T) {
	err := statusError(&http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable"})
	statusErr, ok := err.(*StatusError)
	if !ok || statusErr.StatusCode != http.StatusServiceUnavailable || !strings.Contains(statusErr.Error(), "503 Service Unavailable") {
		t.Fatalf("unexpected status error: %#v", err)
	}

	err = statusError(&http.Response{StatusCode: http.StatusTooManyRequests})
	if !strings.Contains(err.Error(), "status 429") {
		t.Fatalf("numeric status error = %q", err)
	}
	if statusError(nil) != nil {
		t.Fatal("statusError(nil) must be nil")
	}
	var nilError *StatusError
	if nilError.Error() != "<nil>" {
		t.Fatalf("nil StatusError = %q", nilError.Error())
	}
}
