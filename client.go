package httpease

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const defaultMaxErrorBodyBytes int64 = 64 << 10

// ClientOption configures a Client during construction.
type ClientOption func(*clientConfig) error

type clientConfig struct {
	httpClient        *http.Client
	timeout           *time.Duration
	headers           http.Header
	tokenProvider     BearerTokenProvider
	staticBearerToken bool
	maxErrorBodyBytes int64
	allowAbsoluteURLs bool
	retryPolicy       *RetryPolicy
	hooks             []RequestHook
}

// WithTimeout sets the total request timeout. A zero timeout means no limit.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(config *clientConfig) error {
		if timeout < 0 {
			return errors.New("http timeout must not be negative")
		}
		config.timeout = &timeout
		return nil
	}
}

// WithHTTPClient uses a copy of client as the underlying HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(config *clientConfig) error {
		if client == nil {
			return errors.New("http client must not be nil")
		}
		config.httpClient = client
		return nil
	}
}

// WithDefaultHeader adds a header to every request. Per-request headers win.
func WithDefaultHeader(key, value string) ClientOption {
	return func(config *clientConfig) error {
		config.headers.Set(key, value)
		return nil
	}
}

// WithBearerToken adds an Authorization header to every request.
func WithBearerToken(token string) ClientOption {
	return func(config *clientConfig) error {
		if token == "" {
			return errors.New("bearer token must not be empty")
		}
		if config.tokenProvider != nil {
			return errors.New("static bearer token and token provider cannot both be configured")
		}
		config.headers.Set("Authorization", "Bearer "+token)
		config.staticBearerToken = true
		return nil
	}
}

// BearerTokenProvider returns the current Bearer token for a request. The
// provider owns token caching, refresh, and concurrent access.
type BearerTokenProvider func(context.Context) (string, error)

// WithBearerTokenProvider obtains a token before each request. It is intended
// for rotating credentials such as OAuth tokens. A per-request Authorization
// header takes precedence over the provider.
func WithBearerTokenProvider(provider BearerTokenProvider) ClientOption {
	return func(config *clientConfig) error {
		if provider == nil {
			return errors.New("bearer token provider must not be nil")
		}
		if config.staticBearerToken {
			return errors.New("static bearer token and token provider cannot both be configured")
		}
		config.tokenProvider = provider
		return nil
	}
}

// WithMaxErrorBodyBytes limits error response data retained in HTTPError.
func WithMaxErrorBodyBytes(limit int64) ClientOption {
	return func(config *clientConfig) error {
		if limit < 0 {
			return errors.New("maximum error body size must not be negative")
		}
		config.maxErrorBodyBytes = limit
		return nil
	}
}

// WithAbsoluteURLs allows an endpoint to override the client's base URL.
// Keep this disabled unless the target host is explicitly trusted.
func WithAbsoluteURLs() ClientOption {
	return func(config *clientConfig) error {
		config.allowAbsoluteURLs = true
		return nil
	}
}

// WithRetryPolicy enables retries for requests that meet policy requirements.
// Retries are disabled unless this option is supplied.
func WithRetryPolicy(policy RetryPolicy) ClientOption {
	return func(config *clientConfig) error {
		if err := policy.validate(); err != nil {
			return err
		}
		config.retryPolicy = &policy
		return nil
	}
}

// WithRequestHook observes every HTTP attempt, including retries. Hooks must
// return quickly and must not mutate the request or response.
func WithRequestHook(hook RequestHook) ClientOption {
	return func(config *clientConfig) error {
		if hook != nil {
			config.hooks = append(config.hooks, hook)
		}
		return nil
	}
}

// RequestOption configures an individual request.
type RequestOption func(*http.Request) error

// WithHeader sets a request header.
func WithHeader(key, value string) RequestOption {
	return func(request *http.Request) error {
		request.Header.Set(key, value)
		return nil
	}
}

// WithQuery sets query parameters, replacing any matching existing keys.
func WithQuery(values url.Values) RequestOption {
	return func(request *http.Request) error {
		query := request.URL.Query()
		for key, values := range values {
			query.Del(key)
			for _, value := range values {
				query.Add(key, value)
			}
		}
		request.URL.RawQuery = query.Encode()
		return nil
	}
}

// Client is an immutable, concurrent-safe HTTP client after construction.
type Client struct {
	baseURL           *url.URL
	httpClient        *http.Client
	headers           http.Header
	tokenProvider     BearerTokenProvider
	maxErrorBodyBytes int64
	allowAbsoluteURLs bool
	retryPolicy       *RetryPolicy
	hooks             []RequestHook
}

// NewClient creates a client for an absolute HTTP(S) base URL.
func NewClient(baseURL string, options ...ClientOption) (*Client, error) {
	parsedBaseURL, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	config := clientConfig{
		headers:           make(http.Header),
		maxErrorBodyBytes: defaultMaxErrorBodyBytes,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}

	client := &http.Client{Timeout: 60 * time.Second}
	if config.httpClient != nil {
		clientCopy := *config.httpClient
		client = &clientCopy
	}
	if config.timeout != nil {
		client.Timeout = *config.timeout
	}
	return &Client{
		baseURL:           parsedBaseURL,
		httpClient:        client,
		headers:           config.headers.Clone(),
		tokenProvider:     config.tokenProvider,
		maxErrorBodyBytes: config.maxErrorBodyBytes,
		allowAbsoluteURLs: config.allowAbsoluteURLs,
		retryPolicy:       config.retryPolicy,
		hooks:             append([]RequestHook(nil), config.hooks...),
	}, nil
}

func parseBaseURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("base URL must be an absolute http(s) URL")
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed, nil
}

// Do sends a request and returns only successful (2xx) responses. The caller
// must close the returned body.
func (client *Client) Do(ctx context.Context, method, endpoint string, body io.Reader, options ...RequestOption) (*http.Response, error) {
	request, err := client.newRequest(ctx, method, endpoint, body, options...)
	if err != nil {
		return nil, err
	}
	return client.send(request, true)
}

// DoRaw sends a request without interpreting its status code. The caller must
// close the returned body. Use this for streaming and non-JSON protocols.
func (client *Client) DoRaw(ctx context.Context, method, endpoint string, body io.Reader, options ...RequestOption) (*http.Response, error) {
	request, err := client.newRequest(ctx, method, endpoint, body, options...)
	if err != nil {
		return nil, err
	}
	return client.send(request, false)
}

func (client *Client) newRequest(ctx context.Context, method, endpoint string, body io.Reader, options ...RequestOption) (*http.Request, error) {
	if client == nil {
		return nil, errors.New("httpease client must not be nil")
	}
	if ctx == nil {
		return nil, errors.New("request context must not be nil")
	}
	targetURL, err := client.resolveURL(endpoint)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, targetURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header = client.headers.Clone()
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(request); err != nil {
			return nil, fmt.Errorf("apply request option: %w", err)
		}
	}
	if client.tokenProvider != nil && request.Header.Get("Authorization") == "" {
		token, err := client.tokenProvider(ctx)
		if err != nil {
			return nil, fmt.Errorf("get bearer token: %w", err)
		}
		if token == "" {
			return nil, errors.New("bearer token provider returned an empty token")
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request, nil
}

func (client *Client) resolveURL(endpoint string) (*url.URL, error) {
	reference, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if reference.IsAbs() || reference.Host != "" {
		if !client.allowAbsoluteURLs {
			return nil, errors.New("absolute endpoint URLs are disabled")
		}
		if reference.Scheme != "http" && reference.Scheme != "https" {
			return nil, errors.New("absolute endpoint URL must use http(s)")
		}
		return reference, nil
	}

	target := *client.baseURL
	if reference.Path != "" {
		target.Path = path.Join(client.baseURL.Path, strings.TrimPrefix(reference.Path, "/"))
		if strings.HasSuffix(reference.Path, "/") && !strings.HasSuffix(target.Path, "/") {
			target.Path += "/"
		}
	}
	target.RawQuery = reference.RawQuery
	target.Fragment = ""
	return &target, nil
}

func (client *Client) send(request *http.Request, checkStatus bool) (*http.Response, error) {
	if !checkStatus {
		return client.sendOnce(request, 1)
	}
	return client.sendWithRetry(request)
}
