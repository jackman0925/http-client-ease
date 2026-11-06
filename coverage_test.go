package httpease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRawRequestsOptionsAndAbsoluteURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/absolute" {
			t.Errorf("Unexpected target: %s", request.URL)
		}
		if request.URL.Query().Get("page") != "" {
			if request.URL.Query().Get("page") != "2" || request.Header.Get("X-Client") != "override" {
				t.Errorf("Unexpected request options: URL=%s header=%q", request.URL, request.Header.Get("X-Client"))
			}
		}
		writer.WriteHeader(http.StatusTeapot)
		_, _ = writer.Write([]byte("raw response"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/base", WithAbsoluteURLs(), WithDefaultHeader("X-Client", "default"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.DoRaw(context.Background(), http.MethodGet, server.URL+"/absolute?old=1", nil,
		WithQuery(url.Values{"page": {"2"}}), WithHeader("X-Client", "override"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusTeapot || string(body) != "raw response" {
		t.Errorf("Unexpected raw response: status=%d body=%q", response.StatusCode, body)
	}
	if _, err := client.Do(context.Background(), http.MethodGet, server.URL+"/absolute", nil); err == nil {
		t.Error("Expected non-2xx Do error")
	}
	if _, err := client.Do(context.Background(), http.MethodGet, "/absolute", nil, func(*http.Request) error { return errors.New("option failed") }); err == nil {
		t.Error("Expected request option error")
	}
	if _, err := client.Do(context.Background(), http.MethodGet, "ftp://example.com", nil); err == nil {
		t.Error("Expected non-HTTP absolute URL error")
	}
	if _, err := client.Do(nil, http.MethodGet, "/", nil); err == nil {
		t.Error("Expected nil context error")
	}
}

func TestJSONFailureModes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/invalid":
			_, _ = writer.Write([]byte(`{"message":`))
		case "/multiple":
			_, _ = writer.Write([]byte(`{"message":"one"}{"message":"two"}`))
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := DoJSON[testResponse](context.Background(), nil, http.MethodGet, "/", nil); err == nil {
		t.Error("Expected nil client error")
	}
	if _, err := PostJSON[testResponse](context.Background(), client, "/", make(chan int)); err == nil {
		t.Error("Expected JSON marshal error")
	}
	if _, err := GetJSON[testResponse](context.Background(), client, "/invalid"); err == nil {
		t.Error("Expected malformed JSON error")
	}
	if _, err := GetJSON[testResponse](context.Background(), client, "/multiple"); err == nil {
		t.Error("Expected multiple JSON values error")
	}
}

func TestRetryHelpers(t *testing.T) {
	if err := (RetryPolicy{}).validate(); err == nil {
		t.Error("Expected invalid empty retry policy")
	}
	if err := (RetryPolicy{MaxAttempts: 1, BaseDelay: -time.Second}).validate(); err == nil {
		t.Error("Expected invalid delay")
	}
	policy := RetryPolicy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: 100 * time.Millisecond}
	if got := policy.delay(2, nil); got != 100*time.Millisecond {
		t.Errorf("Expected capped retry delay, got %v", got)
	}
	if delay, ok := retryAfterDelay("2", time.Now()); !ok || delay != 2*time.Second {
		t.Errorf("Unexpected Retry-After seconds: %v %t", delay, ok)
	}
	if _, ok := retryAfterDelay("not-a-date", time.Now()); ok {
		t.Error("Expected invalid Retry-After")
	}
	if !retryableMethod(http.MethodGet, false) || retryableMethod(http.MethodPost, false) || !retryableMethod(http.MethodPost, true) {
		t.Error("Unexpected retryable method result")
	}

	request, _ := http.NewRequest(http.MethodPost, "https://example.com", io.NopCloser(strings.NewReader("body")))
	if _, err := replayRequest(request); err == nil {
		t.Error("Expected non-replayable request error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForRetry(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("Expected cancelled retry wait, got %v", err)
	}
}
