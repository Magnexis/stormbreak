package httpretry

import (
	"fmt"
	"net/http"
)

// StatusError describes an HTTP response classified as retryable. The response
// itself remains owned by Transport and is not retained by the error.
type StatusError struct {
	// StatusCode is the numeric HTTP response status.
	StatusCode int
	// Status is the optional response status text, such as "503 Service Unavailable".
	Status string
}

// Error implements error.
func (e *StatusError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Status != "" {
		return fmt.Sprintf("stormbreak/httpretry: retryable HTTP response: %s", e.Status)
	}
	return fmt.Sprintf("stormbreak/httpretry: retryable HTTP status %d", e.StatusCode)
}

func statusError(response *http.Response) error {
	if response == nil {
		return nil
	}
	return &StatusError{StatusCode: response.StatusCode, Status: response.Status}
}
