package httpease

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultRetryBaseDelay = 100 * time.Millisecond
	defaultRetryMaxDelay  = 5 * time.Second
)

// RetryPolicy controls automatic retries. MaxAttempts includes the initial
// request. Retries are limited to idempotent methods unless AllowUnsafeMethods
// is true, and requests with a body must be replayable.
type RetryPolicy struct {
	MaxAttempts        int
	BaseDelay          time.Duration
	MaxDelay           time.Duration
	AllowUnsafeMethods bool
}

func (policy RetryPolicy) validate() error {
	if policy.MaxAttempts < 1 {
		return errors.New("retry maximum attempts must be at least one")
	}
	if policy.BaseDelay < 0 || policy.MaxDelay < 0 {
		return errors.New("retry delays must not be negative")
	}
	return nil
}

// RequestEvent describes one completed HTTP attempt. Response is non-nil when
// an HTTP response was received, even when it has a non-2xx status.
type RequestEvent struct {
	Request  *http.Request
	Response *http.Response
	Error    error
	Attempt  int
	Duration time.Duration
}

// RequestHook observes a completed HTTP attempt.
type RequestHook func(RequestEvent)

func (client *Client) sendWithRetry(request *http.Request) (*http.Response, error) {
	policy := RetryPolicy{MaxAttempts: 1}
	if client.retryPolicy != nil {
		policy = *client.retryPolicy
	}
	currentRequest := request
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		response, err := client.sendOnce(currentRequest, attempt)
		if err == nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return response, nil
		}
		if attempt == policy.MaxAttempts || !policy.shouldRetry(currentRequest, response, err) {
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			return nil, newHTTPError(currentRequest, response, client.maxErrorBodyBytes)
		}
		if response != nil {
			response.Body.Close()
		}
		delay := policy.delay(attempt, response)
		if err := waitForRetry(currentRequest.Context(), delay); err != nil {
			return nil, err
		}
		nextRequest, err := replayRequest(currentRequest)
		if err != nil {
			return nil, err
		}
		currentRequest = nextRequest
	}
	return nil, errors.New("retry loop exhausted")
}

func (client *Client) sendOnce(request *http.Request, attempt int) (*http.Response, error) {
	started := time.Now()
	response, err := client.httpClient.Do(request)
	client.runHooks(RequestEvent{Request: request, Response: response, Error: err, Attempt: attempt, Duration: time.Since(started)})
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	return response, nil
}

func (client *Client) runHooks(event RequestEvent) {
	for _, hook := range client.hooks {
		hook(event)
	}
}

func (policy RetryPolicy) shouldRetry(request *http.Request, response *http.Response, err error) bool {
	if !retryableMethod(request.Method, policy.AllowUnsafeMethods) || !replayable(request) {
		return false
	}
	if err != nil {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}
	return response != nil && retryableStatus(response.StatusCode)
}

func retryableMethod(method string, allowUnsafe bool) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete, http.MethodTrace:
		return true
	case http.MethodPost, http.MethodPatch:
		return allowUnsafe
	default:
		return false
	}
}

func replayable(request *http.Request) bool {
	return request.Body == nil || request.GetBody != nil
}

func (policy RetryPolicy) delay(attempt int, response *http.Response) time.Duration {
	delay := policy.BaseDelay
	if delay == 0 {
		delay = defaultRetryBaseDelay
	}
	maximum := policy.MaxDelay
	if maximum == 0 {
		maximum = defaultRetryMaxDelay
	}
	if delay > maximum {
		delay = maximum
	}
	for repeat := 1; repeat < attempt && delay < maximum; repeat++ {
		if delay > maximum/2 {
			delay = maximum
		} else {
			delay *= 2
		}
	}
	if response != nil {
		if retryAfter, ok := retryAfterDelay(response.Header.Get("Retry-After"), time.Now()); ok && retryAfter > delay {
			delay = retryAfter
		}
	}
	return delay
}

func retryAfterDelay(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if date.Before(now) {
		return 0, true
	}
	return time.Until(date), true
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func replayRequest(request *http.Request) (*http.Request, error) {
	clone := request.Clone(request.Context())
	if request.Body == nil {
		return clone, nil
	}
	if request.GetBody == nil {
		return nil, errors.New("request body cannot be replayed")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, fmt.Errorf("replay request body: %w", err)
	}
	clone.Body = body
	return clone, nil
}

// Retryable reports whether the status is commonly retryable. Callers must
// still account for request idempotency before retrying it.
func (errorResponse *HTTPError) Retryable() bool {
	return retryableStatus(errorResponse.StatusCode)
}

func retryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
