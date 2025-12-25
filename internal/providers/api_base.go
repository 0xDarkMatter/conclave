package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Retry configuration
const (
	maxRetries     = 3
	baseRetryDelay = 1 * time.Second
	maxRetryDelay  = 8 * time.Second
)

// apiBaseProvider provides common functionality for HTTP API-based providers
type apiBaseProvider struct {
	name         string
	defaultModel string
	apiKeyEnv    string
	baseURL      string
	client       *http.Client
}

func (p *apiBaseProvider) Name() string {
	return p.name
}

func (p *apiBaseProvider) DefaultModel() string {
	return p.defaultModel
}

func (p *apiBaseProvider) IsAvailable() bool {
	return os.Getenv(p.apiKeyEnv) != ""
}

func (p *apiBaseProvider) getAPIKey() string {
	return os.Getenv(p.apiKeyEnv)
}

// httpClient returns a shared HTTP client with reasonable defaults
func (p *apiBaseProvider) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{
			Timeout: 120 * time.Second,
		}
	}
	return p.client
}

// isRetryable returns true if the status code indicates a transient error
func isRetryable(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// calculateBackoff returns the delay for a given attempt with jitter
func calculateBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	// Exponential: 1s, 2s, 4s, 8s...
	delay := baseRetryDelay * (1 << (attempt - 1))
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	// Add ±20% jitter
	jitter := float64(delay) * 0.2 * (rand.Float64()*2 - 1)
	return delay + time.Duration(jitter)
}

// parseRetryAfter extracts retry delay from Retry-After header
func parseRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return 0
	}
	// Try parsing as seconds
	if seconds, err := strconv.Atoi(header); err == nil {
		return time.Duration(seconds) * time.Second
	}
	// Could also parse HTTP-date format, but seconds is most common
	return 0
}

// doRequest performs an HTTP request with JSON body and returns the response body.
// It automatically retries on transient errors (429, 5xx) with exponential backoff.
func (p *apiBaseProvider) doRequest(ctx context.Context, method, url string, headers map[string]string, body any) ([]byte, error) {
	// Pre-marshal body so we can retry with same content
	var jsonBody []byte
	if body != nil {
		var err error
		jsonBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check context before attempt
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return nil, fmt.Errorf("%w (after %d retries)", lastErr, attempt)
			}
			return nil, err
		}

		// Wait before retry (not on first attempt)
		if attempt > 0 {
			delay := calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w (after %d retries)", lastErr, attempt)
			case <-time.After(delay):
			}
		}

		// Create fresh request for each attempt
		var bodyReader io.Reader
		if jsonBody != nil {
			bodyReader = bytes.NewReader(jsonBody)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		// Set default headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		// Set custom headers
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := p.httpClient().Do(req)
		if err != nil {
			lastErr = fmt.Errorf("execute request: %w", err)
			continue // Network error, retry
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue // Read error, retry
		}

		// Success
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}

		// Non-retryable error
		if !isRetryable(resp.StatusCode) {
			return nil, parseAPIError(resp.StatusCode, respBody)
		}

		// Retryable error - check Retry-After header
		lastErr = parseAPIError(resp.StatusCode, respBody)
		if retryAfter := parseRetryAfter(resp); retryAfter > 0 && retryAfter < maxRetryDelay {
			// Use server's suggestion if reasonable
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w (after %d retries)", lastErr, attempt+1)
			case <-time.After(retryAfter):
			}
		}
	}

	// All retries exhausted
	if lastErr != nil {
		return nil, fmt.Errorf("%w (after %d retries)", lastErr, maxRetries)
	}
	return nil, fmt.Errorf("request failed after %d retries", maxRetries)
}

// parseAPIError creates a descriptive error from API response
func parseAPIError(statusCode int, body []byte) error {
	// Try to extract error message from common API error formats
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"` // Some APIs use this directly
	}

	if err := json.Unmarshal(body, &errResp); err == nil {
		msg := errResp.Error.Message
		if msg == "" {
			msg = errResp.Message
		}
		if msg != "" {
			return fmt.Errorf("API error (%d): %s", statusCode, msg)
		}
	}

	// Provide helpful messages for common status codes
	switch statusCode {
	case 401:
		return fmt.Errorf("authentication failed (401): invalid API key")
	case 403:
		return fmt.Errorf("forbidden (403): API key lacks permissions")
	case 429:
		return fmt.Errorf("rate limited (429): too many requests")
	case 500, 502, 503:
		return fmt.Errorf("server error (%d): try again later", statusCode)
	default:
		return fmt.Errorf("request failed (%d): %s", statusCode, string(body))
	}
}

// OpenAI-compatible chat completion request/response structures
// Used by OpenAI, Perplexity, Grok, and GLM (they all use this format)

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// extractChatResponse extracts text and metrics from OpenAI-compatible response
func extractChatResponse(data []byte) (string, *Metrics, error) {
	var resp chatCompletionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", nil, fmt.Errorf("parse response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", nil, fmt.Errorf("no choices in response")
	}

	text := resp.Choices[0].Message.Content

	var metrics *Metrics
	if resp.Usage.TotalTokens > 0 {
		metrics = &Metrics{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}

	return text, metrics, nil
}
