package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/pedroborgesdev/papoql/internal/api/client"
	"github.com/pedroborgesdev/papoql/internal/api/config"
	"github.com/pedroborgesdev/papoql/internal/api/database"
	"github.com/pedroborgesdev/papoql/internal/api/logger"
	"github.com/pedroborgesdev/papoql/internal/api/prompts"
	"github.com/pedroborgesdev/papoql/internal/api/repositories"
	"github.com/pedroborgesdev/papoql/internal/api/session"
)

const (
	defaultRulesJSON     = "{\n  \"rules\": [\n    \"Always return the data in a valid format\"\n  ]\n}\n"
	defaultBehaviorsJSON = "{\n  \"behaviors\": [\n    \"Be kind\"\n  ]\n}\n"
	defaultCurrentModel  = "openai/gpt-oss-120b:free"
)

type Service struct {
	mu        sync.Mutex
	client    client.Client
	model     string
	schema    string
	rules     string
	behaviors string
	repo      *repositories.Repository
	initErr   error
}

type QueryPlanning struct {
	Tables  []string
	Columns []string
	Joins   []string
}

func NewService() *Service {
	db, dbErr := database.InitDB()
	service := &Service{
		initErr: dbErr,
	}

	if dbErr != nil {
		logger.Fatalf("Database has not been initialized", []logger.ParamPair{{Key: "error", Value: dbErr.Error()}})
	} else {
		service.repo = repositories.NewRepository(db)
	}

	if err := service.LoadConfig(); err != nil {
		logger.Infof("AI configs hasn't loaded", nil)
	} else {
		logger.Infof("AI configs has been loaded", nil)
	}
	return service
}

func (s *Service) ensureDatabaseReady() error {
	if s.initErr != nil {
		return s.initErr
	}

	if s.repo == nil || s.repo.DB == nil || s.repo.DB.DB == nil {
		return errors.New("database not initialized")
	}

	return nil
}

// formatHistory converts session messages into a context string for the AI.
func formatHistory(history []session.Message) string {
	if len(history) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Conversation history]\n")
	for _, m := range history {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	sb.WriteString("[End of history]\n")
	return sb.String()
}

const (
	MaxContextBytes = 200 * 1024 // 200 KB soft cap for conversation context
)

func computeContextUsageMB(history []session.Message, currentPrompt string) (current float64, max float64) {
	total := len(currentPrompt)
	for _, m := range history {
		total += len(m.Role) + len(m.Content) + 2
	}
	return float64(total) / (1024 * 1024), float64(MaxContextBytes) / (1024 * 1024)
}

func normalizePromptMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "query":
		return "query"
	default:
		return "conversation"
	}
}

func (s *Service) Prompt(ctx context.Context, user_prompt string, mode string, history []session.Message, progressCb ...func(stage string, message string)) (string, string, string, float64, float64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	checkCanceled := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	emit := func(stage string, message string) {
		if len(progressCb) == 0 || progressCb[0] == nil {
			return
		}
		progressCb[0](stage, message)
	}

	currentContextMB, maxContextMB := computeContextUsageMB(history, user_prompt)
	responseMode := normalizePromptMode(mode)
	emit("context", fmt.Sprintf("context: %.4f MB / %.4f MB", currentContextMB, maxContextMB))
	if err := checkCanceled(); err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, err
	}
	if err := s.ensureDatabaseReady(); err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, err
	}

	if s.client == nil {
		return "", "", responseMode, currentContextMB, maxContextMB, errors.New("model not initialized")
	}

	// Build conversation history string for context.
	histStr := formatHistory(history)

	logger.AI("Session Context Usage", []logger.ParamPair{
		{Key: "current_mb", Value: fmt.Sprintf("%.4f", currentContextMB)},
		{Key: "max_mb", Value: fmt.Sprintf("%.4f", maxContextMB)},
	})

	if responseMode == "query" {
		emit("validation", "validating user request")
		if err := checkCanceled(); err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, err
		}
		isValid, validationResp, err := s.getValidationFromAI(s.rules, s.behaviors, user_prompt)
		if err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
		}
		if !isValid {
			logger.AI("AI Validation Failed (query mode)", []logger.ParamPair{{Key: "response", Value: validationResp}})
			return validationResp, "", "conversation", currentContextMB, maxContextMB, nil
		}

		emit("classification", "classifying user intent")
		if err := checkCanceled(); err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, err
		}

		actions, err := s.getActionFromAI(s.rules, histStr, user_prompt, responseMode)
		if err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
		}

		for action, actionType := range actions {
			if actionType == "CONVERSATION" {
				logger.AI("AI Query Classification Fallback", []logger.ParamPair{{Key: "response", Value: action}})
				return action, "", "conversation", currentContextMB, maxContextMB, nil
			}
		}

		emit("action", "processing action_1 (QUERY)")
		enhancedPrompt, _, err := s.getEnhancedPromptFromAI(user_prompt, "QUERY", s.schema)
		if err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
		}
		logger.AI("AI Enhanced Prompt (action_1)", []logger.ParamPair{
			{Key: "enhanced", Value: enhancedPrompt},
			{Key: "needSql", Value: true},
		})

		emit("planning", "planning SQL for action_1")
		if err := checkCanceled(); err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, err
		}

		planning, err := s.getPlanningFromAI(enhancedPrompt)
		if err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
		}
		logger.AI("AI Planning (action_1)", []logger.ParamPair{
			{Key: "tables", Value: strings.Join(planning.Tables, ", ")},
			{Key: "columns", Value: strings.Join(planning.Columns, ", ")},
			{Key: "joins", Value: strings.Join(planning.Joins, ", ")},
		})

		emit("inspection", "inspecting data for action_1")
		if err := checkCanceled(); err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, err
		}
		inspectResult := s.getInspectionFromAI(planning.Tables, planning.Columns, planning.Joins)

		emit("sql_generation", "generating final SQL for action_1")
		if err := checkCanceled(); err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, err
		}

		sqlSteps, err := s.getFinalSQLFromAI(s.rules, enhancedPrompt, s.schema, planning.Tables, planning.Columns, planning.Joins, inspectResult)
		if err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
		}

		sqlStepParams := make([]logger.ParamPair, len(sqlSteps))
		for j, sql := range sqlSteps {
			sqlStepParams[j] = logger.ParamPair{Key: fmt.Sprintf("step_%d", j+1), Value: sql}
		}
		logger.AI("AI SQL Steps (action_1)", sqlStepParams)

		sqlOutput := strings.TrimSpace(strings.Join(sqlSteps, "\n"))
		if sqlOutput == "" {
			return "", "", responseMode, currentContextMB, maxContextMB, errors.New("no SQL generated")
		}

		emit("done", "sql response ready")
		return sqlOutput, sqlOutput, responseMode, currentContextMB, maxContextMB, nil
	}

	// // Step 1B: Validate the prompt based on rules
	// isValid, validationResp, err := s.getValidationFromAI(s.rules, s.behaviors, user_prompt)
	// if err != nil {
	// 	return "", fmt.Errorf("unable to receive a valid response: %s", err.Error())
	// }

	// if !isValid {
	// 	logger.AI("AI Validation Failed", []logger.ParamPair{
	// 		{Key: "response", Value: validationResp},
	// 	})
	// 	return validationResp, nil
	// }

	// Step 1A: Classify actions from user prompt
	emit("classification", "classifying user intent")
	if err := checkCanceled(); err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, err
	}
	actions, err := s.getActionFromAI(s.rules, histStr, user_prompt, responseMode)
	if err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
	}

	// Check if any action is CONVERSATION — return it directly
	for action, actionType := range actions {
		if actionType == "CONVERSATION" {
			logger.AI("AI Conversation Response", []logger.ParamPair{
				{Key: "response", Value: action},
			})
			return action, "", responseMode, currentContextMB, maxContextMB, nil
		}
	}

	// Log all actions
	actionTypeParams := make([]logger.ParamPair, 0, len(actions)*2)
	idx := 1
	for act, typ := range actions {
		actionTypeParams = append(actionTypeParams, logger.ParamPair{Key: fmt.Sprintf("action_%d", idx), Value: act})
		actionTypeParams = append(actionTypeParams, logger.ParamPair{Key: fmt.Sprintf("type_%d", idx), Value: typ})
		idx++
	}
	logger.AI("AI Action/Type", actionTypeParams)

	// Process each action independently, collecting all SQL results
	allSqlResults := make([]interface{}, 0)
	allEnhancedPrompts := make([]string, 0)
	allSqlQueries := make([]string, 0)
	actionIdx := 1

	for action, actionType := range actions {
		emit("action", fmt.Sprintf("processing action_%d (%s)", actionIdx, actionType))
		if err := checkCanceled(); err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, err
		}
		// Step 1B: Enhance the prompt for this specific action
		enhancedPrompt, needSql, err := s.getEnhancedPromptFromAI(action, actionType, s.schema)
		if err != nil {
			return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
		}
		logger.AI(fmt.Sprintf("AI Enhanced Prompt (action_%d)", actionIdx), []logger.ParamPair{
			{Key: "enhanced", Value: enhancedPrompt},
			{Key: "needSql", Value: needSql},
		})
		allEnhancedPrompts = append(allEnhancedPrompts, action)

		needSql = true
		if needSql {
			// Step 2A: Plan which tables/columns/joins are needed
			emit("planning", fmt.Sprintf("planning SQL for action_%d", actionIdx))
			if err := checkCanceled(); err != nil {
				return "", "", responseMode, currentContextMB, maxContextMB, err
			}
			planning, err := s.getPlanningFromAI(action)
			if err != nil {
				return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
			}
			logger.AI(fmt.Sprintf("AI Planning (action_%d)", actionIdx), []logger.ParamPair{
				{Key: "tables", Value: strings.Join(planning.Tables, ", ")},
				{Key: "columns", Value: strings.Join(planning.Columns, ", ")},
				{Key: "joins", Value: strings.Join(planning.Joins, ", ")},
			})

			// Step 2B: Inspect the database to understand actual data
			emit("inspection", fmt.Sprintf("inspecting data for action_%d", actionIdx))
			if err := checkCanceled(); err != nil {
				return "", "", responseMode, currentContextMB, maxContextMB, err
			}
			inspectResult := s.getInspectionFromAI(planning.Tables, planning.Columns, planning.Joins)

			// Step 2C: Generate final SQL using enhanced prompt + schema + planning + inspection
			emit("sql_generation", fmt.Sprintf("generating final SQL for action_%d", actionIdx))
			if err := checkCanceled(); err != nil {
				return "", "", responseMode, currentContextMB, maxContextMB, err
			}
			sqlSteps, err := s.getFinalSQLFromAI(s.rules, action, s.schema, planning.Tables, planning.Columns, planning.Joins, inspectResult)
			if err != nil {
				return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
			}
			sqlStepParams := make([]logger.ParamPair, len(sqlSteps))
			for j, sql := range sqlSteps {
				sqlStepParams[j] = logger.ParamPair{Key: fmt.Sprintf("step_%d", j+1), Value: sql}
			}
			logger.AI(fmt.Sprintf("AI SQL Steps (action_%d)", actionIdx), sqlStepParams)

			allSqlQueries = append(allSqlQueries, sqlSteps...)

			// Execute each SQL step
			for stepIdx, sql := range sqlSteps {
				emit("sql_execute", fmt.Sprintf("executing action_%d_step_%d", actionIdx, stepIdx+1))
				if err := checkCanceled(); err != nil {
					return "", "", responseMode, currentContextMB, maxContextMB, err
				}
				stepMap := map[string]interface{}{
					"step":     fmt.Sprintf("action_%d_step_%d", actionIdx, stepIdx+1),
					"sql":      sql,
					"analysis": nil,
				}
				results, err := s.getSqlResults([]map[string]interface{}{stepMap})
				if err != nil {
					return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to execute SQL step: %s", err.Error())
				}
				allSqlResults = append(allSqlResults, results...)
			}
		}

		actionIdx++
	}

	// Convert allSqlResults to JSON string
	allSqlResultsJSON, err := json.Marshal(allSqlResults)
	if err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to marshal SQL results: %s", err.Error())
	}

	logger.AI("AI All SQL Results", []logger.ParamPair{
		{Key: "results", Value: string(allSqlResultsJSON)},
	})
	emit("nl", "building natural-language response")
	if err := checkCanceled(); err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, err
	}

	// Step 3A: Convert all SQL results to natural language (unified response)
	// combinedEnhanced := strings.Join(allEnhancedPrompts, "\n")
	naturalLanguageResult, err := s.getNaturalLanguageResultFromAI(user_prompt, histStr, allSqlResultsJSON, s.behaviors)
	if err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to receive a valid response: %s", err.Error())
	}
	logger.AI("AI Natural Language Result", []logger.ParamPair{
		{Key: "result", Value: naturalLanguageResult},
	})

	// Step 3B: Sanitize the result based on rules
	if err := checkCanceled(); err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, err
	}
	sanitizedResult, err := s.sanitizeNaturalLanguageResult(s.rules, s.behaviors, histStr, naturalLanguageResult)
	if err != nil {
		return "", "", responseMode, currentContextMB, maxContextMB, fmt.Errorf("unable to sanitize natural language result: %s", err.Error())
	}
	logger.AI("AI Sanitized Result", []logger.ParamPair{
		{Key: "sanitized", Value: sanitizedResult},
	})
	emit("done", "final response ready")

	return sanitizedResult, strings.Join(allSqlQueries, "\n"), responseMode, currentContextMB, maxContextMB, nil
}

func (s *Service) getNaturalLanguageResultFromAI(user_prompt, histStr string, sqlResultsJSON []byte, behaviors string) (string, error) {
	resp, err := s.client.Prompt(
		fmt.Sprintf(prompts.Prompt_3A_LinguagemNatural, histStr, user_prompt, sqlResultsJSON, behaviors),
	)

	if err != nil {
		return "", err
	}

	respStr := fmt.Sprintf("%v", resp)

	if respStr == "" {
		return "", fmt.Errorf("AI returned empty array")
	}

	return respStr, nil
}

func (s *Service) ModelGET() (interface{}, map[string][]string, error) {
	current := s.model

	models := map[string][]string{
		"gemini":       {},
		"openai":       {},
		"hugging_face": {},
		"openrouter":   {},
	}

	for model := range client.GeminiModels {
		models["gemini"] = append(models["gemini"], model)
	}

	for model := range client.OpenaiModels {
		models["openai"] = append(models["openai"], model)
	}

	for model := range client.HuggingFaceModels {
		models["hugging_face"] = append(models["hugging_face"], model)
	}

	for model := range client.OpenRouterModels {
		models["openrouter"] = append(models["openrouter"], model)
	}

	if current == "" {
		return nil, models, nil
	}

	return current, models, nil
}

func (s *Service) ModelPUT(ctx context.Context, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	newClient, err := client.NewAIClient(ctx, model)
	if err != nil {
		return err
	}

	s.client = newClient
	s.model = model

	file, err := os.OpenFile(".papoql/models.json", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	file.Seek(0, 0)
	decoder := json.NewDecoder(file)
	existingConfig := make(map[string]interface{})
	if err := decoder.Decode(&existingConfig); err != nil && err.Error() != "EOF" {
		return err
	}

	existingConfig["current"] = model

	file.Seek(0, 0)
	file.Truncate(0)
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(existingConfig); err != nil {
		return err
	}

	return nil
}

func (s *Service) SchemaPOST() (string, error) {
	if err := s.ensureDatabaseReady(); err != nil {
		return "", err
	}

	err := s.repo.SchemaPOST()

	if err != nil {
		return "", err
	}

	schema, err := s.readSchemaFile()

	return schema, nil
}

func (s *Service) SchemaGET() (string, error) {
	if err := s.ensureDatabaseReady(); err != nil {
		return "", err
	}

	schema, err := s.readSchemaFile()
	if err != nil {
		return "", err
	}

	return schema, nil
}

func (s *Service) LoadConfig() error {
	if err := s.ensureConfigSkeleton(); err != nil {
		return err
	}

	if err := syncModelsAPIKeysFromEnv(); err != nil {
		return err
	}

	if err := client.ReloadModels(); err != nil {
		return err
	}

	file, err := os.Open("./.papoql/models.json")
	if err != nil {
		return err
	}
	defer file.Close()

	type Config struct {
		Model string `json:"current"`
	}

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}

	var newClient client.Client
	if strings.TrimSpace(cfg.Model) != "" {
		newClient, err = client.NewAIClient(context.Background(), cfg.Model)
		if err != nil {
			return err
		}
	}

	rulesFile, err := os.Open("./.papoql/rules.json")
	if err != nil {
		return err
	}
	defer rulesFile.Close()

	content, err := io.ReadAll(rulesFile)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.rules = string(content)
	s.mu.Unlock()

	behaviorsFile, err := os.Open("./.papoql/behaviors.json")
	if err != nil {
		return err
	}
	defer behaviorsFile.Close()

	content, err = io.ReadAll(behaviorsFile)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.behaviors = string(content)
	s.mu.Unlock()

	schemaFile, err := os.Open("./.papoql/schema.txt")
	if err != nil {
		return err
	}
	defer schemaFile.Close()

	content, err = io.ReadAll(schemaFile)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.model = cfg.Model
	s.client = newClient
	s.schema = string(content)

	return nil
}

func (s *Service) ensureConfigSkeleton() error {
	if err := os.MkdirAll("./.papoql", 0755); err != nil {
		return err
	}

	defaultModelsJSON, err := buildDefaultModelsJSON()
	if err != nil {
		return err
	}

	if err := ensureFileWithDefault("./.papoql/models.json", defaultModelsJSON); err != nil {
		return err
	}

	if err := ensureFileWithDefault("./.papoql/rules.json", defaultRulesJSON); err != nil {
		return err
	}

	if err := ensureFileWithDefault("./.papoql/behaviors.json", defaultBehaviorsJSON); err != nil {
		return err
	}

	schemaInfo, err := os.Stat("./.papoql/schema.txt")
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := s.repo.SchemaPOST(); err != nil {
			return err
		}
		return nil
	}

	if schemaInfo.Size() == 0 {
		if err := s.repo.SchemaPOST(); err != nil {
			return err
		}
	}

	return nil
}

func ensureFileWithDefault(path string, content string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}

	if !os.IsNotExist(err) {
		return err
	}

	return os.WriteFile(path, []byte(content), 0644)
}

type modelProviderConfig struct {
	APIKey string   `json:"api_key"`
	Models []string `json:"models"`
}

type modelsConfigFile struct {
	Current string                         `json:"current"`
	Models  map[string]modelProviderConfig `json:"models"`
}

func syncModelsAPIKeysFromEnv() error {
	content, err := os.ReadFile("./.papoql/models.json")
	if err != nil {
		return err
	}

	var cfg modelsConfigFile
	if err := json.Unmarshal(content, &cfg); err != nil {
		return err
	}

	if cfg.Models == nil {
		cfg.Models = make(map[string]modelProviderConfig)
	}

	if strings.TrimSpace(cfg.Current) == "" {
		cfg.Current = defaultCurrentModel
	}

	applyProviderAPIKey(&cfg, "gemini", config.AppConfig.GEMINI_API_KEY, []string{"gemini-2.5-flash-lite", "gemini-2.5-flash", "gemini-2.0-flash-lite"})
	applyProviderAPIKey(&cfg, "openai", config.AppConfig.OPENAI_API_KEY, []string{"gpt-4o"})
	applyProviderAPIKey(&cfg, "hugging_face", config.AppConfig.HUGGINGFACE_API_KEY, []string{"Qwen/Qwen2.5-7B-Instruct", "openai/gpt-oss-120b:groq", "meta-llama/Llama-3.1-8B-Instruct:novita"})
	applyProviderAPIKey(&cfg, "openrouter", config.AppConfig.OPENROUTER_API_KEY, []string{"openai/gpt-oss-120b:free"})

	updated, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile("./.papoql/models.json", append(updated, '\n'), 0644)
}

func applyProviderAPIKey(cfg *modelsConfigFile, providerName, apiKey string, defaultModels []string) {
	provider := cfg.Models[providerName]
	if len(provider.Models) == 0 {
		provider.Models = defaultModels
	}
	provider.APIKey = apiKey
	cfg.Models[providerName] = provider
}

func buildDefaultModelsJSON() (string, error) {
	payload := map[string]interface{}{
		"current": defaultCurrentModel,
		"models": map[string]modelProviderConfig{
			"gemini": {
				APIKey: config.AppConfig.GEMINI_API_KEY,
				Models: []string{"gemini-2.5-flash-lite", "gemini-2.5-flash", "gemini-2.0-flash-lite"},
			},
			"openai": {
				APIKey: config.AppConfig.OPENAI_API_KEY,
				Models: []string{"gpt-4o"},
			},
			"hugging_face": {
				APIKey: config.AppConfig.HUGGINGFACE_API_KEY,
				Models: []string{"Qwen/Qwen2.5-7B-Instruct", "openai/gpt-oss-120b:groq", "meta-llama/Llama-3.1-8B-Instruct:novita"},
			},
			"openrouter": {
				APIKey: config.AppConfig.OPENROUTER_API_KEY,
				Models: []string{"openai/gpt-oss-120b:free"},
			},
		},
	}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b) + "\n", nil
}

func (s *Service) readSchemaFile() (string, error) {
	schema_file, err := os.Open("./.papoql/schema.txt")
	if err != nil {
		return "", err
	}
	defer schema_file.Close()

	content, err := io.ReadAll(schema_file)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func (s *Service) getActionFromAI(rules, histStr, user_prompt, mode string) (map[string]string, error) {
	resp, err := s.client.Prompt(fmt.Sprintf(prompts.Prompt_1B_Classificacao, rules, histStr, mode, user_prompt))
	if err != nil {
		return nil, err
	}

	respStr := fmt.Sprintf("%v", resp)

	type ActionClassification struct {
		Action string `json:"action"`
		Type   string `json:"type"`
	}

	var parsed []ActionClassification
	err = json.Unmarshal([]byte(respStr), &parsed)
	if err != nil {
		return nil, fmt.Errorf("error parsing AI JSON: %w | response: %s", err, respStr)
	}

	if len(parsed) == 0 {
		return nil, fmt.Errorf("AI returned empty array")
	}

	result := make(map[string]string)
	for _, item := range parsed {
		if item.Action != "" && item.Type != "" {
			result[item.Action] = item.Type
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("AI returned empty fields: %s", respStr)
	}

	return result, nil
}

func (s *Service) getEnhancedPromptFromAI(action, actionType, schema string) (string, bool, error) {
	resp, err := s.client.Prompt(
		fmt.Sprintf(prompts.Prompt_1C_Reestruturacao, action, actionType, schema),
	)
	if err != nil {
		return "", false, err
	}

	respStr := fmt.Sprintf("%v", resp)

	type EnhancedPrompt struct {
		Enhanced string `json:"enhanced"`
		SQL      bool   `json:"sql"`
	}

	var parsed []EnhancedPrompt
	if err := json.Unmarshal([]byte(respStr), &parsed); err != nil {
		return "", false, fmt.Errorf("error parsing AI JSON: %w | response: %s", err, respStr)
	}

	if len(parsed) == 0 {
		return "", false, fmt.Errorf("AI returned empty array")
	}

	if parsed[0].Enhanced == "" {
		return "", false, fmt.Errorf("field 'enhanced' is empty: %s", respStr)
	}

	enhanced := parsed[0].Enhanced
	needSQL := parsed[0].SQL

	return enhanced, needSQL, nil
}

func (s *Service) getValidationFromAI(rules, behaviors, question string) (bool, string, error) {
	resp, err := s.client.Prompt(
		fmt.Sprintf(prompts.Prompt_1A_Validacao, question, rules, behaviors),
	)
	if err != nil {
		return false, "", err
	}

	respStr := fmt.Sprintf("%v", resp)

	type ValidationResponse struct {
		Valid    *bool  `json:"valid,omitempty"`
		Response string `json:"response,omitempty"`
	}

	var parsed []ValidationResponse
	if err := json.Unmarshal([]byte(respStr), &parsed); err != nil {
		return false, "", fmt.Errorf("error parsing AI JSON: %w | response: %s", err, respStr)
	}

	if len(parsed) == 0 {
		return false, "", fmt.Errorf("AI returned empty array")
	}

	item := parsed[0]

	if item.Valid != nil && *item.Valid {
		return true, "", nil
	}

	if item.Response != "" {
		return false, item.Response, nil
	}

	return false, "", fmt.Errorf("AI response does not contain 'valid' or 'response'")
}

func (s *Service) getPlanningFromAI(enhancedPrompt string) (QueryPlanning, error) {
	resp, err := s.client.Prompt(
		fmt.Sprintf(prompts.Prompt_2A_Planejamento, enhancedPrompt, s.schema),
	)

	if err != nil {
		return QueryPlanning{}, err
	}

	respStr := fmt.Sprintf("%v", resp)

	type PlanningResponse struct {
		Tables  []string `json:"tables"`
		Columns []string `json:"columns"`
		Joins   []string `json:"joins"`
	}

	var parsed []PlanningResponse
	if err := json.Unmarshal([]byte(respStr), &parsed); err != nil {
		return QueryPlanning{}, fmt.Errorf("error parsing AI JSON: %w | response: %s", err, respStr)
	}

	if len(parsed) == 0 {
		return QueryPlanning{}, fmt.Errorf("AI returned empty array")
	}

	return QueryPlanning{
		Tables:  parsed[0].Tables,
		Columns: parsed[0].Columns,
		Joins:   parsed[0].Joins,
	}, nil
}

func (s *Service) getInspectionFromAI(tables, columns, joins []string) string {
	resp, err := s.client.Prompt(
		fmt.Sprintf(
			prompts.Prompt_2B_Inspecao,
			strings.Join(tables, ", "),
			strings.Join(columns, ", "),
			strings.Join(joins, ", "),
		),
	)

	if err != nil {
		logger.AI("AI Inspection Prompt Error", []logger.ParamPair{{Key: "error", Value: err.Error()}})
		return ""
	}

	respStr := fmt.Sprintf("%v", resp)

	type InspectionResponse struct {
		Step     string      `json:"step"`
		SQL      string      `json:"sql"`
		Analysis interface{} `json:"analysis"`
	}

	var parsed []InspectionResponse
	if err := json.Unmarshal([]byte(respStr), &parsed); err != nil {
		logger.AI("AI Inspection Parse Error", []logger.ParamPair{{Key: "error", Value: err.Error()}})
		return ""
	}
	if len(parsed) == 0 {
		logger.AI("AI Inspection Empty Response", nil)
		return ""
	}

	inspectResult := make([]interface{}, 0, len(parsed))
	for i, p := range parsed {
		stepName := p.Step
		if strings.TrimSpace(stepName) == "" {
			stepName = fmt.Sprintf("inspection_step_%d", i+1)
		}

		logger.AI("AI Inspection Step", []logger.ParamPair{
			{Key: "step", Value: stepName},
			{Key: "sql", Value: p.SQL},
		})

		stepMap := map[string]interface{}{
			"step":     stepName,
			"sql":      p.SQL,
			"analysis": nil,
		}

		stepResult, err := s.getSqlResults([]map[string]interface{}{stepMap})
		if err != nil {
			logger.AI("AI Inspection SQL Execution Error", []logger.ParamPair{{Key: "error", Value: err.Error()}})
			return ""
		}

		inspectResult = append(inspectResult, stepResult...)
	}

	inspectResultStr := ""
	if inspectResult != nil {
		if b, err := json.MarshalIndent(inspectResult, "", "  "); err == nil {
			inspectResultStr = string(b)
		}
	}

	return inspectResultStr
}

func (s *Service) getFinalSQLFromAI(rules, enhancedPrompt, schema string, tables, columns, joins []string, inspection string) ([]string, error) {
	resp, err := s.client.Prompt(
		fmt.Sprintf(
			prompts.Prompt_2C_SQLFinal,
			rules,
			enhancedPrompt,
			schema,
			strings.Join(tables, ", "),
			strings.Join(columns, ", "),
			strings.Join(joins, ", "),
			inspection,
		),
	)

	if err != nil {
		return nil, err
	}

	respStr := fmt.Sprintf("%v", resp)

	type SQLFinalResponse struct {
		Step     string `json:"step"`
		SQL      string `json:"sql"`
		Analysis bool   `json:"analysis,omitempty"`
	}

	var parsed []SQLFinalResponse
	if err := json.Unmarshal([]byte(respStr), &parsed); err != nil {
		return nil, fmt.Errorf("error parsing AI JSON: %w | response: %s", err, respStr)
	}

	if len(parsed) == 0 {
		return nil, fmt.Errorf("AI returned empty array")
	}

	sqls := make([]string, 0, len(parsed))
	for _, p := range parsed {
		if p.SQL != "" {
			sqls = append(sqls, p.SQL)
		}
	}

	return sqls, nil
}

func (s *Service) sanitizeNaturalLanguageResult(rules, behaviors, histStr, result string) (string, error) {
	resp, err := s.client.Prompt(
		fmt.Sprintf(prompts.Prompt_3B_Sanitizacao, rules, behaviors, histStr, result),
	)

	if err != nil {
		return "", err
	}

	respStr := fmt.Sprintf("%v", resp)

	if respStr == "" {
		return "", fmt.Errorf("AI returned empty response")
	}

	// Only allow refusal responses when there is an explicit prohibition rule.
	if isRefusalResponse(respStr) && !hasExplicitProhibitionRule(rules) {
		logger.AI("Sanitizer refusal ignored", []logger.ParamPair{{Key: "reason", Value: "no explicit prohibition rule found"}})
		return result, nil
	}

	return respStr, nil
}

func hasExplicitProhibitionRule(rules string) bool {
	type rulesPayload struct {
		Rules []string `json:"rules"`
	}

	keywords := []string{
		"proib",
		"nao pode",
		"não pode",
		"nao e permitido",
		"não é permitido",
		"forbidden",
		"not allowed",
		"must not",
		"cannot provide",
		"nao fornecer",
		"não fornecer",
		"nao divulgar",
		"não divulgar",
		"nao revelar",
		"não revelar",
	}

	var payload rulesPayload
	if err := json.Unmarshal([]byte(rules), &payload); err == nil && len(payload.Rules) > 0 {
		for _, rule := range payload.Rules {
			normalized := normalizeText(rule)
			for _, kw := range keywords {
				if strings.Contains(normalized, normalizeText(kw)) {
					return true
				}
			}
		}
		return false
	}

	// Fallback: if rules is not JSON, inspect raw text.
	normalized := normalizeText(rules)
	for _, kw := range keywords {
		if strings.Contains(normalized, normalizeText(kw)) {
			return true
		}
	}

	return false
}

func isRefusalResponse(text string) bool {
	normalized := normalizeText(text)
	refusalPatterns := []string{
		"nao e possivel fornecer essa informacao",
		"não é possível fornecer essa informação",
		"nao posso fornecer",
		"não posso fornecer",
		"nao e permitido fornecer",
		"não é permitido fornecer",
	}

	for _, pattern := range refusalPatterns {
		if strings.Contains(normalized, normalizeText(pattern)) {
			return true
		}
	}

	return false
}

func normalizeText(text string) string {
	replacer := strings.NewReplacer(
		"á", "a", "à", "a", "ã", "a", "â", "a",
		"é", "e", "ê", "e",
		"í", "i",
		"ó", "o", "ô", "o", "õ", "o",
		"ú", "u",
		"ç", "c",
	)

	return replacer.Replace(strings.ToLower(strings.TrimSpace(text)))
}

func (s *Service) getSqlResults(jsonSteps []map[string]interface{}) ([]interface{}, error) {
	sqlResults := make([]interface{}, 0, len(jsonSteps))
	for _, step := range jsonSteps {
		sqlStr, _ := step["sql"].(string)

		sqlStrLog := strings.ReplaceAll(sqlStr, "\n", "")
		if len(sqlStrLog) > 80 {
			sqlStrLog = sqlStrLog[:80] + "..."
		}

		item := map[string]interface{}{
			"step": step["step"],
		}

		if sqlStr == "" {
			item["result"] = map[string]interface{}{"error": "no sql found in step"}
			sqlResults = append(sqlResults, item)
			continue
		}

		resBytes, err := s.repo.ExecSQLCommand(sqlStr)

		params := []logger.ParamPair{{Key: "command", Value: sqlStrLog}}

		if err != nil {
			item["result"] = map[string]interface{}{"error": err.Error()}
			sqlResults = append(sqlResults, item)
			logger.SQL(fmt.Sprintf("Executing step '%v'", step["step"]), params)
			continue
		}

		var resObj interface{}
		if resBytes != nil {
			if unerr := json.Unmarshal(resBytes, &resObj); unerr == nil {
				switch v := resObj.(type) {
				case []interface{}:
					cmdUpper := strings.ToUpper(strings.TrimSpace(sqlStr))
					isExec := strings.HasPrefix(cmdUpper, "UPDATE") || strings.HasPrefix(cmdUpper, "DELETE") || strings.HasPrefix(cmdUpper, "INSERT") || strings.HasPrefix(cmdUpper, "REPLACE")
					if isExec {
						item["result"] = "OK"
						params = append(params, logger.ParamPair{Key: "rows", Value: len(v)})
					} else {
						item["result"] = v
						params = append(params, logger.ParamPair{Key: "rows", Value: len(v)})
					}
				case map[string]interface{}:
					if _, hasRA := v["rows_affected"]; hasRA || v["last_insert_id"] != nil {
						item["result"] = "OK"
						if ra, ok := v["rows_affected"]; ok {
							params = append(params, logger.ParamPair{Key: "rows_affected", Value: ra})
						}
						if li, ok := v["last_insert_id"]; ok {
							params = append(params, logger.ParamPair{Key: "last_insert_id", Value: li})
						}
					} else {
						item["result"] = v
						if ra, ok := v["rows_affected"]; ok {
							params = append(params, logger.ParamPair{Key: "rows_affected", Value: ra})
						}
						if li, ok := v["last_insert_id"]; ok {
							params = append(params, logger.ParamPair{Key: "last_insert_id", Value: li})
						}
					}
				default:
					item["result"] = v
					params = append(params, logger.ParamPair{Key: "result", Value: v})
				}
			} else {
				item["result"] = string(resBytes)
				params = append(params, logger.ParamPair{Key: "result_raw", Value: string(resBytes)})
			}
		} else {
			item["result"] = nil
		}

		sqlResults = append(sqlResults, item)

		stepStrLog, _ := step["step"].(string)
		if len(stepStrLog) > 30 {
			stepStrLog = stepStrLog[:30] + "..."
		}
		logger.SQL(fmt.Sprintf("Executing step '%v'", stepStrLog), params)
	}

	return sqlResults, nil
}
