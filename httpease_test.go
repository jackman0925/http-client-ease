package httpease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testResponse is a standard struct for test responses.
type testResponse struct {
	Message string `json:"message"`
	Value   int    `json:"value"`
}

// setupTestServer creates a httptest.Server with a configurable handler.
func setupTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestClient(t *testing.T) {
	// Common response for successful requests
	successResponse := testResponse{Message: "success", Value: 123}
	successResponseBody, _ := json.Marshal(successResponse)

	// Common handler for successful requests
	successHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for custom header
		if authHeader := r.Header.Get("Authorization"); authHeader != "Bearer test-token" {
			http.Error(w, "missing or invalid authorization header", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(successResponseBody)
	})

	t.Run("Successful POST request", func(t *testing.T) {
		server := setupTestServer(t, successHandler)
		defer server.Close()

		client := NewClient(server.URL)
		requestBody := map[string]string{"data": "test"}

		resp, err := Post[testResponse](context.Background(), client, "/", requestBody, WithHeader("Authorization", "Bearer test-token"))

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		if resp.Message != successResponse.Message || resp.Value != successResponse.Value {
			t.Errorf("Expected response %+v, but got %+v", successResponse, resp)
		}
	})

	t.Run("Successful GET request", func(t *testing.T) {
		server := setupTestServer(t, successHandler)
		defer server.Close()

		client := NewClient(server.URL)

		resp, err := Get[testResponse](context.Background(), client, "/data", WithHeader("Authorization", "Bearer test-token"))

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		if resp.Message != successResponse.Message {
			t.Errorf("Expected message 'success', but got '%s'", resp.Message)
		}
	})

	t.Run("Request with full URL endpoint", func(t *testing.T) {
		server := setupTestServer(t, successHandler)
		defer server.Close()

		// Base URL is different, but we provide the full URL in the request
		client := NewClient("http://some-other-url.com")
		fullURL := server.URL + "/full/path"

		resp, err := Get[testResponse](context.Background(), client, fullURL, WithHeader("Authorization", "Bearer test-token"))

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		if resp.Message != successResponse.Message {
			t.Errorf("Expected message 'success', but got '%s'", resp.Message)
		}
	})

	t.Run("HTTP error handling", func(t *testing.T) {
		errorBody := `{"error": "not found"}`
		errorHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(errorBody))
		})

		server := setupTestServer(t, errorHandler)
		defer server.Close()

		client := NewClient(server.URL)
		_, err := Get[testResponse](context.Background(), client, "/notfound")

		if err == nil {
			t.Fatal("Expected an error, but got nil")
		}

		var httpErr *HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("Expected error of type *HTTPError, but got %T", err)
		}

		if httpErr.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status code %d, but got %d", http.StatusNotFound, httpErr.StatusCode)
		}
		if !strings.Contains(string(httpErr.Body), "not found") {
			t.Errorf("Expected error body to contain 'not found', but got '%s'", string(httpErr.Body))
		}
		expectedErrorMsg := fmt.Sprintf("http error: status code 404, status 404 Not Found, body: %s", errorBody)
		if httpErr.Error() != expectedErrorMsg {
			t.Errorf("Expected error message '%s', but got '%s'", expectedErrorMsg, httpErr.Error())
		}
	})

	t.Run("Context cancellation", func(t *testing.T) {
		server := setupTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond) // Ensure the request is in-flight when context is canceled
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()


		client := NewClient(server.URL)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel context immediately

		_, err := Get[testResponse](ctx, client, "/")

		if err == nil {
			t.Fatal("Expected an error for canceled context, but got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, but got: %v", err)
		}
	})

	t.Run("Client with custom timeout", func(t *testing.T) {
		server := setupTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond) // This will be longer than the client's timeout
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create a client with a very short timeout
		client := NewClient(server.URL, WithTimeout(50*time.Millisecond))

		_, err := Get[testResponse](context.Background(), client, "/")

		if err == nil {
			t.Fatal("Expected a timeout error, but got nil")
		}
		// The error should be a url.Error indicating a timeout
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("Expected error to be a timeout error, but got: %v", err)
		}
	})
}
