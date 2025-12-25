package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GeminiAPIProvider implements the Google Gemini API
type GeminiAPIProvider struct {
	apiBaseProvider
	keyRotator *KeyRotator
}

// NewGeminiAPIProvider creates a new Gemini API provider
func NewGeminiAPIProvider() *GeminiAPIProvider {
	return &GeminiAPIProvider{
		apiBaseProvider: apiBaseProvider{
			name:         "gemini",
			defaultModel: "gemini-3-pro-preview", // Gemini 3 Pro (preview)
			apiKeyEnv:    "GEMINI_API_KEY",
			baseURL:      "https://generativelanguage.googleapis.com",
		},
		keyRotator: NewKeyRotator("GEMINI_API_KEY", "GOOGLE_API_KEY"),
	}
}

// IsAvailable checks for GEMINI_API_KEY or GOOGLE_API_KEY
func (p *GeminiAPIProvider) IsAvailable() bool {
	return p.keyRotator.HasKeys()
}

func (p *GeminiAPIProvider) getAPIKey() string {
	return p.keyRotator.Next()
}

// Gemini-specific request/response structures
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Query executes a prompt using Gemini API
// POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent?key={API_KEY}
func (p *GeminiAPIProvider) Query(ctx context.Context, prompt string, model string) (string, time.Duration, *Metrics, error) {
	if model == "" {
		model = p.defaultModel
	}

	start := time.Now()

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
	}

	// Gemini uses query parameter for API key
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, model, p.getAPIKey())

	respBody, err := p.doRequest(ctx, "POST", url, nil, reqBody)
	duration := time.Since(start)

	if err != nil {
		return "", duration, nil, err
	}

	text, metrics, err := p.parseResponse(respBody)
	if err != nil {
		return "", duration, nil, err
	}

	return text, duration, metrics, nil
}

func (p *GeminiAPIProvider) parseResponse(data []byte) (string, *Metrics, error) {
	var resp geminiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", nil, fmt.Errorf("parse response: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return "", nil, fmt.Errorf("no candidates in response")
	}

	// Extract text from parts
	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		text += part.Text
	}

	if text == "" {
		return "", nil, fmt.Errorf("no text content in response")
	}

	var metrics *Metrics
	if resp.UsageMetadata.TotalTokenCount > 0 {
		metrics = &Metrics{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		}
	}

	return text, metrics, nil
}
