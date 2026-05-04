package dto

type Prompt struct {
	Prompt string `json:"prompt" binding:"required"`
	Mode   string `json:"mode"`
}

type PromptCancel struct {
	RequestID string `json:"request_id" binding:"required"`
}

type Models struct {
	Model string `json:"model" binding:"required"`
}

type Humanize struct {
	Prompt string `json:"prompt" binding:"required"`
	Model  string `json:"model" binding:"required"`
	ApiKey string `json:"api_key:" binding:"required"`
}
