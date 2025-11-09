package httpease

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// === Client Configuration ===

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithTimeout sets the timeout for the http client.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithHttpClient allows providing a custom http.Client.
func WithHttpClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// === Request Configuration ===

// RequestOption is a functional option for configuring an http.Request.
type RequestOption func(*http.Request)

// WithHeader adds a header to the request.
func WithHeader(key, value string) RequestOption {
	return func(req *http.Request) {
		req.Header.Set(key, value)
	}
}

// === HTTP Client ===

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// HTTPError contains detailed information about a non-200 response.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http error: status code %d, status %s, body: %s", e.StatusCode, e.Status, string(e.Body))
}

// NewClient creates a new Client with the given base URL and options.
/* 例子：
// 1. 定义响应结构体
type GenerateResponse struct {
	Response string `json:"response"`
}

// 2. 创建客户端, 自定义超时
client := httpease.NewClient("http://localhost:11434", httpease.WithTimeout(30*time.Second))

// 3. 创建带超时的上下文
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

// 4. 准备请求体
requestBody := map[string]any{"prompt": "Why is the sky blue?"}

// 5. 发起请求, 并通过 WithHeader 添加自定义请求头
response, err := httpease.Post[GenerateResponse](ctx, client, "/api/generate", requestBody, httpease.WithHeader("Authorization", "Bearer my-token"))
if err != nil {
    // 处理自定义错误
    var httpErr *httpease.HTTPError
    if errors.As(err, &httpErr) {
        log.Fatalf("Request failed with status %d: %s", httpErr.StatusCode, string(httpErr.Body))
    }
    log.Fatal("Error making POST request:", err)
}

// 6. 直接使用类型安全的响应
fmt.Println("Response:", response.Response)
*/
func NewClient(baseURL string, opts ...ClientOption) *Client {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	client := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // 默认超时60秒
		},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

func doRequest[T any](ctx context.Context, c *Client, method, endpoint string, body any, reqOpts ...RequestOption) (*T, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	ref, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}

	finalURL := base.ResolveReference(ref).String()

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("error marshaling JSON: %w", err)
		}
		reqBody = bytes.NewBuffer(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, finalURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for _, opt := range reqOpts {
		opt(req)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("received non-2xx status (%s), but failed to read response body: %w", resp.Status, readErr)
		}
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       respBody,
		}
	}

	var responseBody T
	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		return nil, fmt.Errorf("error decoding response JSON: %w", err)
	}

	return &responseBody, nil
}

func Get[T any](ctx context.Context, c *Client, endpoint string, reqOpts ...RequestOption) (*T, error) {
	return doRequest[T](ctx, c, "GET", endpoint, nil, reqOpts...)
}

func Post[T any](ctx context.Context, c *Client, endpoint string, body any, reqOpts ...RequestOption) (*T, error) {
	return doRequest[T](ctx, c, "POST", endpoint, body, reqOpts...)
}

func Put[T any](ctx context.Context, c *Client, endpoint string, body any, reqOpts ...RequestOption) (*T, error) {
	return doRequest[T](ctx, c, "PUT", endpoint, body, reqOpts...)
}

func Delete[T any](ctx context.Context, c *Client, endpoint string, body any, reqOpts ...RequestOption) (*T, error) {
	return doRequest[T](ctx, c, "DELETE", endpoint, body, reqOpts...)
}
