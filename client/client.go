// Package client provides an HTTP client for communicating with the TaskForge API.
// It includes support for authentication, retries with exponential backoff,
// and proper context handling.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/agincgit/forgeworker/retry"
	"github.com/rs/zerolog"
)

// Client is an HTTP client for the TaskForge API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	retryConfig retry.Config
	log        zerolog.Logger
}

// Config holds client configuration.
type Config struct {
	BaseURL     string
	Token       string
	Timeout     time.Duration
	RetryConfig *retry.Config
}

// New creates a new TaskForge API client.
func New(cfg Config, log zerolog.Logger) *Client {
	retryConfig := retry.DefaultConfig()
	if cfg.RetryConfig != nil {
		retryConfig = *cfg.RetryConfig
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &Client{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		token:       cfg.Token,
		retryConfig: retryConfig,
		log:         log.With().Str("component", "api-client").Logger(),
	}
}

// Request represents an HTTP request to the API.
type Request struct {
	Method  string
	Path    string
	Body    interface{}
	Headers map[string]string
}

// Response represents an HTTP response from the API.
type Response struct {
	StatusCode int
	Body       []byte
}

// Do executes an HTTP request with retries.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	var resp *Response
	var lastErr error

	err := retry.Do(ctx, c.retryConfig, func(ctx context.Context) error {
		var err error
		resp, err = c.doOnce(ctx, req)
		if err != nil {
			return err
		}

		// Check for retryable status codes
		if resp.StatusCode >= 500 {
			return retry.Retryable(fmt.Errorf("server error: %d", resp.StatusCode))
		}
		if resp.StatusCode == 429 {
			return retry.Retryable(fmt.Errorf("rate limited"))
		}

		return nil
	}, c.log)

	if err != nil {
		return nil, err
	}

	return resp, lastErr
}

// doOnce executes a single HTTP request without retries.
func (c *Client) doOnce(ctx context.Context, req Request) (*Response, error) {
	url := c.baseURL + req.Path

	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return &Response{
		StatusCode: httpResp.StatusCode,
		Body:       body,
	}, nil
}

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodGet,
		Path:   path,
	})
}

// Post performs a POST request.
func (c *Client) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	})
}

// Put performs a PUT request.
func (c *Client) Put(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodPut,
		Path:   path,
		Body:   body,
	})
}

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodDelete,
		Path:   path,
	})
}

// DecodeJSON decodes the response body as JSON.
func (r *Response) DecodeJSON(v interface{}) error {
	return json.Unmarshal(r.Body, v)
}

// IsSuccess returns true if the response indicates success (2xx).
func (r *Response) IsSuccess() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}
