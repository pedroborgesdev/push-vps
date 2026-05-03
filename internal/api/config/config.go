package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"

	"github.com/pedroborgesdev/papoql/internal/api/logger"
)

type Config struct {
	HTTP_PORT           string
	DB_PATH             string
	GEMINI_API_KEY      string
	OPENAI_API_KEY      string
	HUGGINGFACE_API_KEY string
	OPENROUTER_API_KEY  string
}

var AppConfig Config

func LoadAppConfig() error {

	err := godotenv.Load()
	if err != nil {
		logger.Debugf("Error on read .env file", []logger.ParamPair{{Key: "Error", Value: err.Error()}})

	}

	AppConfig = Config{
		HTTP_PORT:           getEnvStr("HTTP_PORT", "8801"),
		DB_PATH:             getEnvStr("DB_PATH", ""),
		GEMINI_API_KEY:      getEnvStr("GEMINI_API_KEY", ""),
		OPENAI_API_KEY:      getEnvStr("OPENAI_API_KEY", ""),
		HUGGINGFACE_API_KEY: getEnvStr("HUGGINGFACE_API_KEY", ""),
		OPENROUTER_API_KEY:  getEnvStr("OPENROUTER_API_KEY", ""),
	}

	if AppConfig.DB_PATH == "" {
		return fmt.Errorf("DB_PATH is not defined on .env")
	}

	return nil
}

func getEnvStr(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return boolValue
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intValue
}
