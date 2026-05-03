package client

import (
	"context"
	"errors"

	"github.com/pedroborgesdev/papoql/internal/api/logger"
	"github.com/sashabaranov/go-openai"
)

type OpenRouterAIClient struct {
	Model  string
	client *openai.Client
}

func NewOpenRouterAIClient(model, apiKey string) *OpenRouterAIClient {
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://openrouter.ai/api/v1"

	return &OpenRouterAIClient{
		Model:  model,
		client: openai.NewClientWithConfig(config),
	}
}

func (o *OpenRouterAIClient) Prompt(prompt string) (string, error) {
	logger.AI("Sending prompt", nil)

	ctx := context.Background()
	resp, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: o.Model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("no response from model")
	}

	logger.AI("Response received", nil)

	return resp.Choices[0].Message.Content, nil
}
