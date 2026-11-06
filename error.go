package httpease

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HTTPError describes a non-2xx response. Body is retained as raw bytes, so it
// can contain JSON, HTML, or plain text. It is limited by WithMaxErrorBodyBytes.
type HTTPError struct {
	StatusCode  int
	Status      string
	Header      http.Header
	Method      string
	URL         string
	RequestID   string
	ContentType string
	RetryAfter  string
	Body        []byte
	Truncated   bool
}

func (errorResponse *HTTPError) Error() string {
	message := fmt.Sprintf("%s %s: %s", errorResponse.Method, errorResponse.URL, errorResponse.Status)
	if errorResponse.RequestID != "" {
		message += " (request_id=" + errorResponse.RequestID + ")"
	}
	return message
}

func newHTTPError(request *http.Request, response *http.Response, limit int64) error {
	body, truncated, err := readErrorBody(response.Body, limit)
	if err != nil {
		return fmt.Errorf("read error response body: %w", err)
	}
	return &HTTPError{
		StatusCode:  response.StatusCode,
		Status:      response.Status,
		Header:      response.Header.Clone(),
		Method:      request.Method,
		URL:         redactedURL(request.URL),
		RequestID:   requestID(response.Header),
		ContentType: response.Header.Get("Content-Type"),
		RetryAfter:  response.Header.Get("Retry-After"),
		Body:        body,
		Truncated:   truncated,
	}
}

func readErrorBody(body io.Reader, limit int64) ([]byte, bool, error) {
	if limit == int64(^uint64(0)>>1) {
		data, err := io.ReadAll(body)
		return data, false, err
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) <= limit {
		return data, false, nil
	}
	return data[:limit], true, nil
}

func requestID(header http.Header) string {
	for _, key := range []string{"X-Request-ID", "X-Correlation-ID", "X-Amzn-Trace-Id"} {
		if value := header.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func redactedURL(target *url.URL) string {
	copy := *target
	if copy.User != nil {
		copy.User = url.User("REDACTED")
	}
	query := copy.Query()
	for key := range query {
		switch strings.ToLower(key) {
		case "access_token", "api_key", "apikey", "authorization", "password", "secret", "token":
			query.Set(key, "REDACTED")
		}
	}
	copy.RawQuery = query.Encode()
	return copy.String()
}
