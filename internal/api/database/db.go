package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/pedroborgesdev/papoql/internal/api/config"
	"github.com/pedroborgesdev/papoql/internal/api/logger"

	_ "modernc.org/sqlite"
)

type Database struct {
	DB *sql.DB
}

func InitDB() (*Database, error) {
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
