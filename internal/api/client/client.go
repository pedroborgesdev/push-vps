package client

import (
	"context"
	"encoding/json"
	"errors"
	"os"
)

type Client interface {
	Prompt(prompt string) (string, error)
}

var (
	GeminiModels      = map[string]bool{}
	OpenaiModels      = map[string]bool{}
	HuggingFaceModels = map[string]bool{}
	OpenRouterModels  = map[string]bool{}
	ModelAPIKeys      = map[string]string{}
)

type modelsFile struct {
	Models map[string]struct {
		Models []string `json:"models"`
		APIKey string   `json:"api_key"`
	} `json:"models"`
}

func init() {
	_ = loadModelsFromFile()
}

func ReloadModels() error {
	return loadModelsFromFile()
}

func loadModelsFromFile() error {
	b, err := os.ReadFile(".papoql/models.json")
	if err != nil {
		return err
	}

	var mf modelsFile
	if err := json.Unmarshal(b, &mf); err != nil {
		return err
	}

	GeminiModels = map[string]bool{}
	OpenaiModels = map[string]bool{}
	HuggingFaceModels = map[string]bool{}
	OpenRouterModels = map[string]bool{}
	ModelAPIKeys = map[string]string{}

	if gem, ok := mf.Models["gemini"]; ok {
		for _, m := range gem.Models {
			GeminiModels[m] = true
		}
		ModelAPIKeys["gemini"] = gem.APIKey
	}

	if oa, ok := mf.Models["openai"]; ok {
		for _, m := range oa.Models {
			OpenaiModels[m] = true
		}
		ModelAPIKeys["openai"] = oa.APIKey
	}

	if hf, ok := mf.Models["hugging_face"]; ok {
		for _, m := range hf.Models {
			HuggingFaceModels[m] = true
		}
		ModelAPIKeys["hugging_face"] = hf.APIKey
	}

	if or, ok := mf.Models["openrouter"]; ok {
		for _, m := range or.Models {
			OpenRouterModels[m] = true
		}
		ModelAPIKeys["openrouter"] = or.APIKey
	}

	return nil
}

func apiKeyForModel(model string) (string, bool) {
	if GeminiModels[model] {
		k, ok := ModelAPIKeys["gemini"]
		return k, ok
	}
	if OpenaiModels[model] {
		k, ok := ModelAPIKeys["openai"]
		return k, ok
	}
	if HuggingFaceModels[model] {
		k, ok := ModelAPIKeys["hugging_face"]
		return k, ok
	}
	if OpenRouterModels[model] {
		k, ok := ModelAPIKeys["openrouter"]
		return k, ok
	}

	return "", false
}

func NewAIClient(ctx context.Context, model string) (Client, error) {
	var api_key string
	if k, ok := apiKeyForModel(model); ok {
		api_key = k
	}

	if GeminiModels[model] {
		return NewGeminiClient(ctx, model, api_key)
	}

	if OpenaiModels[model] {
		return NewOpenAIClient(model, api_key), nil
	}

	if HuggingFaceModels[model] {
		return NewHuggineFaceAIClient(model, api_key), nil
	}

	if OpenRouterModels[model] {
		return NewOpenRouterAIClient(model, api_key), nil
	}

	return nil, errors.New("unsupported model")
}
