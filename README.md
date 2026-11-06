# http-client-ease

`http-client-ease` is a small, opinionated HTTP client for company services. It makes JSON calls concise while preserving enough structured information to diagnose API, gateway, and Nginx failures.

## Install

```bash
go get github.com/jackman0925/http-client-ease
```

## JSON requests

```go
package main

import (
	"context"
	"errors"
	"log"
	"time"

	httpease "github.com/jackman0925/http-client-ease"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func main() {
	client, err := httpease.NewClient(
		"https://api.example.internal/v1",
		httpease.WithTimeout(10*time.Second),
		httpease.WithBearerToken("token"),
	)
	if err != nil {
		log.Fatal(err)
	}

	response, err := httpease.GetJSON[User](context.Background(), client, "/users/42")
	if err != nil {
		var httpError *httpease.HTTPError
		if errors.As(err, &httpError) {
			log.Printf("request failed: status=%d request_id=%s content_type=%s body=%q",
				httpError.StatusCode, httpError.RequestID, httpError.ContentType, httpError.Body)
		}
		log.Fatal(err)
	}

	// Data is nil for 204 No Content and other empty successful responses.
	if response.Data != nil {
		log.Printf("user: %+v", *response.Data)
	}
}
```

The JSON helpers are `GetJSON`, `HeadJSON`, `OptionsJSON`, `PostJSON`, `PutJSON`, `PatchJSON`, `DeleteJSON`, and `DoJSON`. Go does not support type parameters on methods, so these are package functions that accept a configured `*Client`.

For a copy-ready application template covering GET, POST, PUT, DELETE, authentication, retries, and structured error handling, see [examples/example.go](examples/example.go). Set `API_BASE_URL` and `API_TOKEN`, then adapt the endpoint paths and payload structs.

## Errors and gateway responses

Every non-2xx response becomes `*HTTPError`, whether its body is JSON, HTML, or plain text. It includes:

- status code and status text;
- request method and a query-redacted URL;
- response headers, content type, request/correlation ID, and `Retry-After`;
- the raw error body and a `Truncated` flag.

Error bodies are retained up to 64 KiB by default. Set `WithMaxErrorBodyBytes` to change the limit. `HTTPError.Error()` deliberately does not include the response body, so logging the error alone does not expose an HTML error page or server details. `Retryable()` identifies commonly retryable status codes; callers must still only retry requests that are safe to repeat.

## Raw requests

For downloads, uploads, streaming, or non-JSON protocols, use the configured client directly:

```go
response, err := client.Do(ctx, http.MethodGet, "/reports/latest.csv", nil)
if err != nil {
	log.Fatal(err) // non-2xx is *httpease.HTTPError
}
defer response.Body.Close()
```

`DoRaw` returns responses for all HTTP status codes when the caller must inspect an error response body directly. In both cases, callers close the returned body.

## Authentication, retries, and observability

Use `WithBearerToken` for static Bearer authentication. For other schemes, use `WithDefaultHeader` or a custom `http.Client` transport.

Retries are disabled by default. To opt in, configure a `RetryPolicy`:

```go
client, err := httpease.NewClient(
	"https://api.example.internal",
	httpease.WithRetryPolicy(httpease.RetryPolicy{
		MaxAttempts: 3, // includes the first attempt
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   2 * time.Second,
	}),
	httpease.WithRequestHook(func(event httpease.RequestEvent) {
		// Called once per completed HTTP attempt, including retries.
		metrics.Record(event.Request.Method, event.Response, event.Error, event.Duration)
	}),
)
```

The policy retries common transient statuses (`408`, `425`, `429`, `500`, `502`, `503`, `504`) and transport failures. It retries only idempotent methods (`GET`, `HEAD`, `OPTIONS`, `PUT`, `DELETE`, `TRACE`) by default, and only when the request body can be replayed. Set `AllowUnsafeMethods: true` only after the target API provides an idempotency key or equivalent duplicate-protection guarantee. `Retry-After` is respected when it asks for a longer delay.

`RequestHook` is intentionally observational: it receives the request, raw response or transport error, attempt number, and duration. Hooks must return quickly and must not mutate the request or response. `DoRaw` does not retry because it hands its response body directly to the caller.

## Security and URL rules

Relative endpoints are joined to the base URL even when written with a leading slash: a base URL of `https://host/v1` and endpoint `/users` produces `https://host/v1/users`.

Absolute endpoint URLs are disabled by default to prevent a request from accidentally bypassing the configured service. Enable them only for trusted targets with `WithAbsoluteURLs()`.

## Breaking upgrade notes

This release intentionally changes the API:

1. Handle the `error` returned by `NewClient`.
2. Replace `Get[T]`/`Post[T]` with `GetJSON[T]`/`PostJSON[T]`.
3. Read JSON data from `response.Data`; it is nil for a successful empty response.
4. Use `errors.As(err, &httpError)` for structured non-2xx diagnostics. Do not parse `err.Error()`.
