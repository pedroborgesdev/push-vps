package client

import (
	"context"
	"errors"

	"github.com/pedroborgesdev/papoql/internal/api/logger"
	"github.com/sashabaranov/go-openai"
)

type OpenAIClient struct {
	Model  string
	client *openai.Client
}

func NewOpenAIClient(model, apiKey string) *OpenAIClient {
	return &OpenAIClient{
		Model:  model,
		client: openai.NewClient(apiKey),
	}
}

func (o *OpenAIClient) Prompt(prompt string) (string, error) {
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
		return "", errors.New("no response from OpenAI")
	}

	logger.AI("Response received", nil)

	return resp.Choices[0].Message.Content, nil
}
