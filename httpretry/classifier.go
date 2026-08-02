// Package httpretry provides an HTTP transport protected by a shared stormbreak budget.
package httpretry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
)

// DefaultClassifier retries temporary transport failures and selected overload or server statuses.
func DefaultClassifier(response *http.Response, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return true
		}
		var networkError net.Error
		return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
	}
	if response == nil {
		return false
	}
	switch response.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retrySafeRequest(request *http.Request) bool {
	if request.Body != nil && request.Body != http.NoBody && request.GetBody == nil {
		return false
	}
	switch request.Method {
	case "", http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete:
		return true
	case http.MethodPost, http.MethodPatch:
		return request.Header.Get("Idempotency-Key") != ""
	default:
		return false
	}
}
