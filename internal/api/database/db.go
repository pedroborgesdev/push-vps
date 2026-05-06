package database

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/pedroborgesdev/papoql/internal/api/config"
	"github.com/pedroborgesdev/papoql/internal/api/logger"

	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

type Database struct {
	DB *sql.DB
}

func InitDB() (*Database, error) {
	engine := strings.ToLower(strings.TrimSpace(config.AppConfig.DB_ENGINE))
	if engine == "" {
		engine = "sqlite"
	}

	if engine == "sqlserver" {
		return initSQLServerDB()
	}

	return initSQLiteDB()
}

func initSQLiteDB() (*Database, error) {
	if config.AppConfig.DB_PATH != ":memory:" {
		info, err := os.Stat(config.AppConfig.DB_PATH)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logger.Errorf("Database file does not exist", []logger.ParamPair{{Key: "db_path", Value: config.AppConfig.DB_PATH}})
				return nil, fmt.Errorf("database file does not exist: %s", config.AppConfig.DB_PATH)
			}

			logger.Errorf("Failed to access database path", []logger.ParamPair{{Key: "error", Value: err.Error()}, {Key: "db_path", Value: config.AppConfig.DB_PATH}})
			return nil, fmt.Errorf("failed to access database path: %w", err)
		}

		if info.IsDir() {
			logger.Errorf("Database path points to a directory", []logger.ParamPair{{Key: "db_path", Value: config.AppConfig.DB_PATH}})
			return nil, fmt.Errorf("database path points to a directory: %s", config.AppConfig.DB_PATH)
		}
	}

	db, err := sql.Open("sqlite", config.AppConfig.DB_PATH)
	if err != nil {
		logger.Errorf("Failed to open database", []logger.ParamPair{{Key: "error", Value: err.Error()}, {Key: "db_path", Value: config.AppConfig.DB_PATH}})
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		logger.Errorf("Failed to ping database", []logger.ParamPair{{Key: "error", Value: err.Error()}})
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Database{DB: db}, nil
}

func initSQLServerDB() (*Database, error) {
	query := url.Values{}
	if value := strings.TrimSpace(config.AppConfig.SQLSERVER_ENCRYPT); value != "" {
		query.Set("encrypt", value)
	}
	query.Set("TrustServerCertificate", fmt.Sprintf("%t", config.AppConfig.SQLSERVER_TRUST_CERT))

	dsnURL := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(config.AppConfig.SQLSERVER_USER, config.AppConfig.SQLSERVER_PASSWORD),
		Host:     fmt.Sprintf("%s:%d", config.AppConfig.SQLSERVER_HOST, config.AppConfig.SQLSERVER_PORT),
		RawQuery: query.Encode(),
	}

	if dbName := strings.TrimSpace(config.AppConfig.SQLSERVER_DATABASE); dbName != "" {
		dsnURL.Path = dbName
	}

	db, err := sql.Open("sqlserver", dsnURL.String())
	if err != nil {
		logger.Errorf("Failed to open SQL Server database", []logger.ParamPair{{Key: "error", Value: err.Error()}, {Key: "host", Value: config.AppConfig.SQLSERVER_HOST}, {Key: "database", Value: config.AppConfig.SQLSERVER_DATABASE}})
		return nil, fmt.Errorf("failed to open sqlserver database: %w", err)
	}

	if err := db.Ping(); err != nil {
		logger.Errorf("Failed to ping SQL Server database", []logger.ParamPair{{Key: "error", Value: err.Error()}, {Key: "host", Value: config.AppConfig.SQLSERVER_HOST}, {Key: "database", Value: config.AppConfig.SQLSERVER_DATABASE}})
		return nil, fmt.Errorf("failed to ping sqlserver database: %w", err)
	}

	return &Database{DB: db}, nil
}
