# http-client-ease

`http-client-ease` is a Go library designed to simplify making RESTful HTTP requests, making it easier for Go projects to integrate with RESTful APIs.

## Features

-   **Type-Safe Responses**: Automatically unmarshals JSON responses into your Go structs.
-   **Functional Options**: Easily configure HTTP clients (e.g., timeouts, custom `http.Client`) and requests (e.g., headers).
-   **Context-Aware**: Supports `context.Context` for request cancellation and timeouts.
-   **Error Handling**: Provides a custom `HTTPError` type for detailed information on non-2xx responses.

## Installation

To install the library, use `go get`:

```bash
go get http-client-ease
```

## Usage

Here's a quick example of how to use `httpease` to make a POST request:

```go
package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackman0925/http-client-ease/httpease" // Import the library
)

// Define your response struct
type GenerateResponse struct {
	Response string `json:"response"`
}

func main() {
	// 1. Create a new client with a custom timeout
	client := httpease.NewClient("http://localhost:11434", httpease.WithTimeout(30*time.Second))

	// 2. Create a context with a timeout for the request
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 3. Prepare the request body
	requestBody := map[string]any{"prompt": "Why is the sky blue?"}

	// 4. Make a POST request, adding a custom header
	response, err := httpease.Post[GenerateResponse](
		ctx,
		client,
		"/api/generate",
		requestBody,
		httpease.WithHeader("Authorization", "Bearer my-token"),
	)
	if err != nil {
		// Handle custom HTTP errors
		var httpErr *httpease.HTTPError
		if errors.As(err, &httpErr) {
			log.Fatalf("Request failed with status %d: %s", httpErr.StatusCode, string(httpErr.Body))
		}
		log.Fatal("Error making POST request:", err)
	}

	// 5. Use the type-safe response
	fmt.Println("Response:", response.Response)
}
```

### Client Options

-   `httpease.WithTimeout(duration time.Duration)`: Sets the timeout for the underlying `http.Client`.
-   `httpease.WithHttpClient(client *http.Client)`: Provides a custom `http.Client` instance.

### Request Options

-   `httpease.WithHeader(key, value string)`: Adds a custom header to the request.

## Error Handling

The library returns a custom `*httpease.HTTPError` for non-2xx HTTP responses, allowing you to inspect the status code and response body.

```go
if err != nil {
    var httpErr *httpease.HTTPError
    if errors.As(err, &httpErr) {
        log.Printf("HTTP Error: Status %d, Body: %s", httpErr.StatusCode, string(httpErr.Body))
    } else {
        log.Printf("General Error: %v", err)
    }
}
```