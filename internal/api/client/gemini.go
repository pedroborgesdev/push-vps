package client

import (
	"context"
	"errors"

	"github.com/pedroborgesdev/papoql/internal/api/logger"
	"google.golang.org/genai"
)

type GeminiClient struct {
	Model  string
	client *genai.Client
}

func NewGeminiClient(ctx context.Context, model, apiKey string) (*GeminiClient, error) {
	cli, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, err
	}

	return &GeminiClient{
		Model:  model,
		client: cli,
	}, nil
}

func (g *GeminiClient) Prompt(prompt string) (string, error) {
	logger.AI("Sending prompt", nil)

	ctx := context.Background()

	resp, err := g.client.Models.GenerateContent(
		ctx,
		g.Model,
		genai.Text(prompt),
		nil,
	)
	if err != nil {
		return "", err
	}

	if resp == nil {
		return "", errors.New("no response from Gemini")
	}

	logger.AI("Response received", nil)

	return resp.Text(), nil
}
