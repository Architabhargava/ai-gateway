package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
)

// callAIWithURL is a testable version of callAI that accepts a custom URL.
// Used in unit tests to point at a mock Groq server instead of the real one.
func (g *Gateway) callAIWithURL(prompt, url string) (string, error) {
	client := resty.New()
	client.SetTimeout(10 * time.Second)

	reqBody := groqRequest{
		Model: "llama-3.3-70b-versatile",
		Messages: []groqMessage{
			{Role: "user", Content: prompt},
		},
	}

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Authorization", "Bearer "+g.apiKey).
		SetBody(reqBody).
		Post(url)

	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("groq returned HTTP %d: %s", resp.StatusCode(), string(resp.Body()))
	}

	var groqResp groqResponse
	if err := json.Unmarshal(resp.Body(), &groqResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if groqResp.Error != nil {
		return "", fmt.Errorf("groq error: %s", groqResp.Error.Message)
	}

	if len(groqResp.Choices) == 0 {
		return "", fmt.Errorf("no response from Groq")
	}

	return groqResp.Choices[0].Message.Content, nil
}

// Ensure io is used (suppress unused import if resty handles body)
var _ = io.Discard
