package httpease

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Response is a successful HTTP response with decoded JSON data. Data is nil
// for an empty response, including 204 No Content.
type Response[T any] struct {
	Data       *T
	StatusCode int
	Status     string
	Header     http.Header
	RequestID  string
}

// DoJSON sends a JSON request and decodes its JSON response.
func DoJSON[T any](ctx context.Context, client *Client, method, endpoint string, body any, options ...RequestOption) (*Response[T], error) {
	if client == nil {
		return nil, errors.New("httpease client must not be nil")
	}

	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request JSON: %w", err)
		}
		requestBody = bytes.NewReader(data)
	}

	request, err := client.newRequest(ctx, method, endpoint, requestBody, options...)
	if err != nil {
		return nil, err
	}
	if body != nil && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.send(request, true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	result := &Response[T]{
		StatusCode: response.StatusCode,
		Status:     response.Status,
		Header:     response.Header.Clone(),
		RequestID:  requestID(response.Header),
	}
	if response.StatusCode == http.StatusNoContent {
		return result, nil
	}

	var data T
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&data); errors.Is(err, io.EOF) {
		return result, nil
	} else if err != nil {
		return nil, fmt.Errorf("decode response JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode response JSON: response contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode response JSON: %w", err)
	}
	result.Data = &data
	return result, nil
}

// GetJSON sends a GET request and decodes its JSON response.
func GetJSON[T any](ctx context.Context, client *Client, endpoint string, options ...RequestOption) (*Response[T], error) {
	return DoJSON[T](ctx, client, http.MethodGet, endpoint, nil, options...)
}

// HeadJSON sends a HEAD request and returns its response metadata.
func HeadJSON[T any](ctx context.Context, client *Client, endpoint string, options ...RequestOption) (*Response[T], error) {
	return DoJSON[T](ctx, client, http.MethodHead, endpoint, nil, options...)
}

// OptionsJSON sends an OPTIONS request and decodes its JSON response.
func OptionsJSON[T any](ctx context.Context, client *Client, endpoint string, options ...RequestOption) (*Response[T], error) {
	return DoJSON[T](ctx, client, http.MethodOptions, endpoint, nil, options...)
}

// PostJSON sends a POST request with a JSON body and decodes its JSON response.
func PostJSON[T any](ctx context.Context, client *Client, endpoint string, body any, options ...RequestOption) (*Response[T], error) {
	return DoJSON[T](ctx, client, http.MethodPost, endpoint, body, options...)
}

// PutJSON sends a PUT request with a JSON body and decodes its JSON response.
func PutJSON[T any](ctx context.Context, client *Client, endpoint string, body any, options ...RequestOption) (*Response[T], error) {
	return DoJSON[T](ctx, client, http.MethodPut, endpoint, body, options...)
}

// PatchJSON sends a PATCH request with a JSON body and decodes its JSON response.
func PatchJSON[T any](ctx context.Context, client *Client, endpoint string, body any, options ...RequestOption) (*Response[T], error) {
	return DoJSON[T](ctx, client, http.MethodPatch, endpoint, body, options...)
}

// DeleteJSON sends a DELETE request and decodes its JSON response.
func DeleteJSON[T any](ctx context.Context, client *Client, endpoint string, body any, options ...RequestOption) (*Response[T], error) {
	return DoJSON[T](ctx, client, http.MethodDelete, endpoint, body, options...)
}
