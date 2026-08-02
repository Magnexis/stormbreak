package httpretry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magnexis/stormbreak"
)

func TestClientIntegrationRetriesServerFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(response, "temporarily unavailable")
			return
		}
		_, _ = io.WriteString(response, "recovered")
	}))
	defer server.Close()

	client := &http.Client{Transport: &Transport{Budget: httpBudget(t, 2), Policy: httpPolicy(3)}}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != "recovered" || calls.Load() != 2 {
		t.Fatalf("status=%d body=%q calls=%d", response.StatusCode, body, calls.Load())
	}
}

func TestClientIntegrationReplaysIdempotentPost(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var receivedBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		mu.Lock()
		receivedBodies = append(receivedBodies, string(body))
		mu.Unlock()
		if calls.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := &http.Client{Transport: &Transport{Budget: httpBudget(t, 1), Policy: httpPolicy(2)}}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader("payload"))
	request.Header.Set("Idempotency-Key", "request-123")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	mu.Lock()
	bodies := append([]string(nil), receivedBodies...)
	mu.Unlock()
	if response.StatusCode != http.StatusCreated || calls.Load() != 2 || len(bodies) != 2 || bodies[0] != "payload" || bodies[1] != "payload" {
		t.Fatalf("status=%d calls=%d bodies=%v", response.StatusCode, calls.Load(), bodies)
	}
}

func TestClientIntegrationDoesNotRetryUnsafePost(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &http.Client{Transport: &Transport{Budget: httpBudget(t, 2), Policy: httpPolicy(3)}}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader("unsafe"))
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatalf("status=%d calls=%d", response.StatusCode, calls.Load())
	}
}

func TestClientIntegrationCancellationDuringBackoff(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	transport := &Transport{
		Budget: httpBudget(t, 1),
		Policy: stormbreak.Policy{MaxAttempts: 2, BaseDelay: time.Hour, MaxDelay: time.Hour, Multiplier: 1},
		Hooks:  stormbreak.Hooks{OnRetry: func(stormbreak.RetryEvent) { cancel() }},
	}
	client := &http.Client{Transport: transport}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	response, err := client.Do(request)
	if response != nil || !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("response=%v err=%v calls=%d", response, err, calls.Load())
	}
}
