package gateway

import (
	"encoding/json"
	"fmt"

	"github.com/go-resty/resty/v2"
)

type groqRequest struct {
	Model    string        `json:"model"`
	Messages []groqMessage `json:"messages"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *Gateway) callAI(prompt string) (string, error) {
	client := resty.New()

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
		Post("https://api.groq.com/openai/v1/chat/completions")

	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	var groqResp groqResponse
	err = json.Unmarshal(resp.Body(), &groqResp)
	if err != nil {
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
