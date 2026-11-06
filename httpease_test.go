package httpease

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type testResponse struct {
	Message string `json:"message"`
}

func TestJSONRequestAndResponseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/users" {
			t.Errorf("Unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer default" {
			t.Errorf("Unexpected authorization header: %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Unexpected Content-Type: %q", request.Header.Get("Content-Type"))
		}
		writer.Header().Set("X-Request-ID", "request-123")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"message":"ok"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api", WithDefaultHeader("Authorization", "Bearer default"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := PostJSON[testResponse](context.Background(), client, "/users", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Data == nil || response.Data.Message != "ok" {
		t.Fatalf("Unexpected decoded response: %+v", response.Data)
	}
	if response.StatusCode != http.StatusOK || response.RequestID != "request-123" {
		t.Errorf("Unexpected response metadata: %+v", response)
	}
}

func TestEmptyJSONResponseHasNilData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	response, err := DeleteJSON[testResponse](context.Background(), client, "/users/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.Data != nil || response.StatusCode != http.StatusNoContent {
		t.Errorf("Expected empty 204 response, got %+v", response)
	}
}

func TestJSONContentTypeIsBodyAwareAndOverridable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/without-body":
			if contentType := request.Header.Get("Content-Type"); contentType != "" {
				t.Errorf("GET unexpectedly sent Content-Type %q", contentType)
			}
		case "/custom-content-type":
			if contentType := request.Header.Get("Content-Type"); contentType != "application/problem+json" {
				t.Errorf("Custom Content-Type was ignored: %q", contentType)
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"message":"ok"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := GetJSON[testResponse](context.Background(), client, "/without-body"); err != nil {
		t.Fatal(err)
	}
	if _, err := PostJSON[testResponse](context.Background(), client, "/custom-content-type", map[string]string{"a": "b"}, WithHeader("Content-Type", "application/problem+json")); err != nil {
		t.Fatal(err)
	}
}

func TestHeadAndOptionsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			if request.Method != http.MethodHead {
				t.Errorf("Expected HEAD, got %s", request.Method)
			}
			writer.Header().Set("X-Health", "ok")
		case "/capabilities":
			if request.Method != http.MethodOptions {
				t.Errorf("Expected OPTIONS, got %s", request.Method)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"message":"GET, POST"}`))
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	head, err := HeadJSON[testResponse](context.Background(), client, "/health")
	if err != nil || head.Data != nil || head.Header.Get("X-Health") != "ok" {
		t.Errorf("Unexpected HEAD response: response=%+v error=%v", head, err)
	}
	options, err := OptionsJSON[testResponse](context.Background(), client, "/capabilities")
	if err != nil || options.Data == nil || options.Data.Message != "GET, POST" {
		t.Errorf("Unexpected OPTIONS response: response=%+v error=%v", options, err)
	}
}

func TestRetryPolicyRetriesSafeRequestsAndRunsHooks(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if attempts.Add(1) < 3 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"message":"recovered"}`))
	}))
	defer server.Close()

	var hookAttempts atomic.Int32
	client, err := NewClient(server.URL,
		WithRetryPolicy(RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}),
		WithRequestHook(func(event RequestEvent) {
			if event.Response != nil {
				hookAttempts.Add(1)
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := GetJSON[testResponse](context.Background(), client, "/")
	if err != nil {
		t.Fatal(err)
	}
	if response.Data == nil || response.Data.Message != "recovered" || attempts.Load() != 3 || hookAttempts.Load() != 3 {
		t.Errorf("Unexpected retry result: response=%+v attempts=%d hooks=%d", response, attempts.Load(), hookAttempts.Load())
	}
}

func TestRetryPolicyDoesNotRetryPostByDefault(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithRetryPolicy(RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}

	_, err = PostJSON[testResponse](context.Background(), client, "/", map[string]string{"order": "1"})
	var httpError *HTTPError
	if !errors.As(err, &httpError) || attempts.Load() != 1 {
		t.Errorf("POST should not be retried by default: error=%v attempts=%d", err, attempts.Load())
	}
}

func TestBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Errorf("Unexpected authorization: %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"message":"ok"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithBearerToken("token-123"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GetJSON[testResponse](context.Background(), client, "/"); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayHTMLErrorIncludesDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("X-Request-ID", "nginx-42")
		writer.Header().Set("Retry-After", "30")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("<html><title>Bad Gateway</title></html>"))
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	_, err = GetJSON[testResponse](context.Background(), client, "/upstream?token=super-secret")
	var httpError *HTTPError
	if !errors.As(err, &httpError) {
		t.Fatalf("Expected HTTPError, got %T: %v", err, err)
	}
	if httpError.ContentType != "text/html; charset=utf-8" || httpError.RequestID != "nginx-42" || httpError.RetryAfter != "30" {
		t.Errorf("Missing gateway diagnostics: %+v", httpError)
	}
	if !bytes.Contains(httpError.Body, []byte("Bad Gateway")) || !httpError.Retryable() {
		t.Errorf("Unexpected error body or retryability: %+v", httpError)
	}
	if bytes.Contains([]byte(httpError.Error()), []byte("super-secret")) || bytes.Contains([]byte(httpError.Error()), httpError.Body) {
		t.Errorf("Error() leaked sensitive content: %q", httpError.Error())
	}
}

func TestErrorBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte("abcdefgh"))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithMaxErrorBodyBytes(4))
	if err != nil {
		t.Fatal(err)
	}

	_, err = GetJSON[testResponse](context.Background(), client, "/")
	var httpError *HTTPError
	if !errors.As(err, &httpError) {
		t.Fatalf("Expected HTTPError, got %T", err)
	}
	if !httpError.Truncated || string(httpError.Body) != "abcd" {
		t.Errorf("Expected truncated error body, got %+v", httpError)
	}
}

func TestRawRequestsAndURLPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/download" {
			t.Errorf("Unexpected path: %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("raw"))
	}))
	defer server.Close()
	client, err := NewClient(server.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Do(context.Background(), http.MethodPost, "/download", bytes.NewBufferString("payload"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "raw" {
		t.Errorf("Unexpected raw response: %q", body)
	}
	if _, err := client.DoRaw(context.Background(), http.MethodGet, server.URL, nil); err == nil {
		t.Error("Expected absolute endpoint to be rejected")
	}
}

func TestConstructionValidation(t *testing.T) {
	if _, err := NewClient("not-a-url"); err == nil {
		t.Error("Expected invalid base URL error")
	}
	if _, err := NewClient("https://example.com", WithTimeout(-1)); err == nil {
		t.Error("Expected invalid timeout error")
	}
	if _, err := NewClient("https://example.com", WithHTTPClient(nil)); err == nil {
		t.Error("Expected nil client error")
	}
}

func TestCustomHTTPClientSettingsArePreserved(t *testing.T) {
	transport := &http.Transport{}
	customClient := &http.Client{Transport: transport}
	client, err := NewClient("https://example.com", WithHTTPClient(customClient))
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Transport != transport || client.httpClient.Timeout != 0 {
		t.Errorf("Custom HTTP client settings were not preserved: %+v", client.httpClient)
	}
}
