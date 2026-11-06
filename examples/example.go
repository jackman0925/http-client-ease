// Package main demonstrates how an application integrates http-client-ease.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	httpease "github.com/jackman0925/http-client-ease"
)

// Replace these types with the request and response payloads of your API.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func main() {
	baseURL := os.Getenv("API_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.example.internal/v1"
	}

	client, err := httpease.NewClient(
		baseURL,
		httpease.WithTimeout(10*time.Second),
		httpease.WithBearerToken(os.Getenv("API_TOKEN")),
		httpease.WithRetryPolicy(httpease.RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   200 * time.Millisecond,
			MaxDelay:    2 * time.Second,
		}),
	)
	if err != nil {
		log.Fatalf("configure HTTP client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	user, err := getUser(ctx, client, "user-42")
	if err != nil {
		logError(err)
		return
	}
	fmt.Printf("GET user: %+v\n", user)

	created, err := createUser(ctx, client, CreateUserRequest{Name: "Ada", Email: "ada@example.com"})
	if err != nil {
		logError(err)
		return
	}
	fmt.Printf("POST user: %+v\n", created)

	updated, err := updateUser(ctx, client, created.ID, CreateUserRequest{Name: "Ada Lovelace", Email: "ada@example.com"})
	if err != nil {
		logError(err)
		return
	}
	fmt.Printf("PUT user: %+v\n", updated)

	if err := deleteUser(ctx, client, updated.ID); err != nil {
		logError(err)
		return
	}
	fmt.Println("DELETE user: success")
}

func getUser(ctx context.Context, client *httpease.Client, id string) (*User, error) {
	response, err := httpease.GetJSON[User](ctx, client, "/users/"+id)
	if err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, errors.New("get user: empty response")
	}
	return response.Data, nil
}

func createUser(ctx context.Context, client *httpease.Client, input CreateUserRequest) (*User, error) {
	response, err := httpease.PostJSON[User](ctx, client, "/users", input)
	if err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, errors.New("create user: empty response")
	}
	return response.Data, nil
}

func updateUser(ctx context.Context, client *httpease.Client, id string, input CreateUserRequest) (*User, error) {
	response, err := httpease.PutJSON[User](ctx, client, "/users/"+id, input)
	if err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, errors.New("update user: empty response")
	}
	return response.Data, nil
}

func deleteUser(ctx context.Context, client *httpease.Client, id string) error {
	response, err := httpease.DeleteJSON[struct{}](ctx, client, "/users/"+id, nil)
	if err != nil {
		return err
	}
	if response.StatusCode != 204 {
		return fmt.Errorf("delete user: unexpected status %d", response.StatusCode)
	}
	return nil
}

func logError(err error) {
	var httpError *httpease.HTTPError
	if errors.As(err, &httpError) {
		log.Printf("HTTP request failed: method=%s url=%s status=%d request_id=%s content_type=%s body=%q",
			httpError.Method, httpError.URL, httpError.StatusCode, httpError.RequestID,
			httpError.ContentType, httpError.Body)
		return
	}
	log.Printf("HTTP transport or decoding error: %v", err)
}
