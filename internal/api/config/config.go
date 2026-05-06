package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"github.com/pedroborgesdev/papoql/internal/api/logger"
)

type Config struct {
	HTTP_PORT            string
	DB_ENGINE            string
	DB_PATH              string
	SQLSERVER_HOST       string
	SQLSERVER_PORT       int
	SQLSERVER_DATABASE   string
	SQLSERVER_USER       string
	SQLSERVER_PASSWORD   string
	SQLSERVER_ENCRYPT    string
	SQLSERVER_TRUST_CERT bool
	SECRET_KEY           string
	GEMINI_API_KEY       string
	OPENAI_API_KEY       string
	HUGGINGFACE_API_KEY  string
	OPENROUTER_API_KEY   string
}

var AppConfig Config

func LoadAppConfig() error {

	err := godotenv.Load()
	if err != nil {
		logger.Debugf("Error on read .env file", []logger.ParamPair{{Key: "Error", Value: err.Error()}})

	}

	AppConfig = Config{
		HTTP_PORT:            getEnvStr("HTTP_PORT", "8801"),
		DB_ENGINE:            getEnvStr("DB_ENGINE", "sqlite"),
		DB_PATH:              getEnvStr("DB_PATH", ""),
		SQLSERVER_HOST:       getEnvStr("SQLSERVER_HOST", ""),
		SQLSERVER_PORT:       getEnvInt("SQLSERVER_PORT", 1433),
		SQLSERVER_DATABASE:   getEnvStr("SQLSERVER_DATABASE", ""),
		SQLSERVER_USER:       getEnvStr("SQLSERVER_USER", ""),
		SQLSERVER_PASSWORD:   getEnvStr("SQLSERVER_PASSWORD", ""),
		SQLSERVER_ENCRYPT:    getEnvStr("SQLSERVER_ENCRYPT", "disable"),
		SQLSERVER_TRUST_CERT: getEnvBool("SQLSERVER_TRUST_CERT", true),
		SECRET_KEY:           getEnvStr("SECRET_KEY", "default_secret_key"),
		GEMINI_API_KEY:       getEnvStr("GEMINI_API_KEY", ""),
		OPENAI_API_KEY:       getEnvStr("OPENAI_API_KEY", ""),
		HUGGINGFACE_API_KEY:  getEnvStr("HUGGINGFACE_API_KEY", ""),
		OPENROUTER_API_KEY:   getEnvStr("OPENROUTER_API_KEY", ""),
	}

	engine := normalizeDBEngine(AppConfig.DB_ENGINE)
	if engine == "sqlite" {
		if AppConfig.DB_PATH == "" {
			return fmt.Errorf("DB_PATH is not defined on .env")
		}
		return nil
	}

	if engine == "sqlserver" {
		if AppConfig.SQLSERVER_HOST == "" || AppConfig.SQLSERVER_DATABASE == "" || AppConfig.SQLSERVER_USER == "" {
			return fmt.Errorf("SQLSERVER_HOST, SQLSERVER_DATABASE and SQLSERVER_USER must be defined for DB_ENGINE=sqlserver")
		}
		return nil
	}

	return fmt.Errorf("unsupported DB_ENGINE: %s", AppConfig.DB_ENGINE)
}

func normalizeDBEngine(value string) string {
	engine := value
	if engine == "" {
		engine = "sqlite"
	}
	return strings.ToLower(strings.TrimSpace(engine))
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
