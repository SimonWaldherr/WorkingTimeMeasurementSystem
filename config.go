package main

import (
	"fmt"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
)

// Config holds application configuration
type Config struct {
	Database DatabaseConfig `json:"database"`
	Server   ServerConfig   `json:"server"`
	Security SecurityConfig `json:"security"`
	Features FeatureConfig  `json:"features"`
}

type DatabaseConfig struct {
	Backend  string `json:"backend"`  // sqlite, mssql, mariadb
	SQLite   SQLiteConfig `json:"sqlite"`
	MSSQL    MSSQLConfig  `json:"mssql"`
	MariaDB  MariaDBConfig `json:"mariadb"`
	AutoMigrate bool `json:"auto_migrate"`
}

type SQLiteConfig struct {
	Path string `json:"path"`
}

type MSSQLConfig struct {
	Server   string `json:"server"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}

type MariaDBConfig struct {
	Host     string `json:"host"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}

type ServerConfig struct {
	Port           int    `json:"port"`
	Host           string `json:"host"`
	ReadTimeout    int    `json:"read_timeout"`
	WriteTimeout   int    `json:"write_timeout"`
	IdleTimeout    int    `json:"idle_timeout"`
	MaxHeaderBytes int    `json:"max_header_bytes"`
}

type SecurityConfig struct {
	SessionSecret   string `json:"session_secret"`
	SessionDuration int    `json:"session_duration"` // in minutes
	CSRFProtection  bool   `json:"csrf_protection"`
	RateLimiting    bool   `json:"rate_limiting"`
}

type FeatureConfig struct {
	MultiTenant    bool `json:"multi_tenant"`
	BarcodeScanning bool `json:"barcode_scanning"`
	Reporting      bool `json:"reporting"`
	EmailNotifications bool `json:"email_notifications"`
	ClockMode      string `json:"clock_mode"` // input | button | both
}

var appConfig *Config

// initConfig initializes the application configuration
func initConfig() {
	config := &Config{
		Database: DatabaseConfig{
			Backend: getEnv("DB_BACKEND", "sqlite"),
			SQLite: SQLiteConfig{
				Path: getEnv("SQLITE_PATH", "time_tracking.db"),
			},
			MSSQL: MSSQLConfig{
				Server:   getEnv("MSSQL_SERVER", "sql-cluster-05"),
				Database: getEnv("MSSQL_DATABASE", "wtm"),
				User:     getEnv("MSSQL_USER", "johndoe"),
				Password: getEnv("MSSQL_PASSWORD", "secret"),
				Port:     getEnvInt("MSSQL_PORT", 1433),
			},
			MariaDB: MariaDBConfig{
				Host:     getEnv("MARIADB_HOST", "127.0.0.1"),
				Database: getEnv("MARIADB_DATABASE", "wtm"),
				User:     getEnv("MARIADB_USER", "wtm"),
				Password: getEnv("MARIADB_PASSWORD", "secret"),
				Port:     getEnvInt("MARIADB_PORT", 3306),
			},
			AutoMigrate: getEnvBool("DB_AUTO_MIGRATE", true),
		},
		Server: ServerConfig{
			Port:           getEnvInt("SERVER_PORT", 8083),
			Host:           getEnv("SERVER_HOST", ""),
			ReadTimeout:    getEnvInt("SERVER_READ_TIMEOUT", 15),
			WriteTimeout:   getEnvInt("SERVER_WRITE_TIMEOUT", 15),
			IdleTimeout:    getEnvInt("SERVER_IDLE_TIMEOUT", 60),
			MaxHeaderBytes: getEnvInt("SERVER_MAX_HEADER_BYTES", 1048576),
		},
		Security: SecurityConfig{
			SessionSecret:   getEnv("SESSION_SECRET", "change-me-very-secret"),
			SessionDuration: getEnvInt("SESSION_DURATION", 30),
			CSRFProtection:  getEnvBool("CSRF_PROTECTION", false),
			RateLimiting:    getEnvBool("RATE_LIMITING", false),
		},
		Features: FeatureConfig{
			MultiTenant:        getEnvBool("FEATURE_MULTI_TENANT", true),
			BarcodeScanning:    getEnvBool("FEATURE_BARCODE_SCANNING", true),
			Reporting:          getEnvBool("FEATURE_REPORTING", true),
			EmailNotifications: getEnvBool("FEATURE_EMAIL_NOTIFICATIONS", false),
			ClockMode:          strings.ToLower(getEnv("FEATURE_CLOCK_MODE", "both")),
		},
	}

	// Try to load from config file if it exists
	if configData, err := os.ReadFile("config.json"); err == nil {
		var fileConfig Config
		if err := json.Unmarshal(configData, &fileConfig); err == nil {
			// Merge file config with environment config
			mergeConfigs(config, &fileConfig)
		}
	}

	appConfig = config

	// Update global variables for backward compatibility
	dbBackend = config.Database.Backend
	sqlitePath = config.Database.SQLite.Path
	mssqlServer = config.Database.MSSQL.Server
	mssqlDB = config.Database.MSSQL.Database
	mssqlUser = config.Database.MSSQL.User
	mssqlPass = config.Database.MSSQL.Password
	mssqlPort = config.Database.MSSQL.Port
	mariadbHost = config.Database.MariaDB.Host
	mariadbDB = config.Database.MariaDB.Database
	mariadbUser = config.Database.MariaDB.User
	mariadbPass = config.Database.MariaDB.Password
	mariadbPort = config.Database.MariaDB.Port
}

// getConfig returns the current configuration
func getConfig() *Config {
	if appConfig == nil {
		initConfig()
	}
	return appConfig
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// mergeConfigs merges file config into environment config
func mergeConfigs(envConfig, fileConfig *Config) {
	// Only override environment config if file value is not zero value
	if fileConfig.Database.Backend != "" {
		envConfig.Database.Backend = fileConfig.Database.Backend
	}
	if fileConfig.Database.SQLite.Path != "" {
		envConfig.Database.SQLite.Path = fileConfig.Database.SQLite.Path
	}
	if fileConfig.Database.MSSQL.Server != "" {
		envConfig.Database.MSSQL.Server = fileConfig.Database.MSSQL.Server
	}
	// ... similar for other config fields
}

// saveConfig saves current configuration to file
func saveConfig() error {
	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("config.json", data, 0644)
}

// validateConfig validates the configuration
func validateConfig() error {
	config := getConfig()
	
	// Validate database configuration
	switch strings.ToLower(config.Database.Backend) {
	case "sqlite", "mssql", "mariadb", "mysql":
		// ok
	default:
		return fmt.Errorf("invalid database backend: %s", config.Database.Backend)
	}
	
	// Validate server configuration
	if config.Server.Port <= 0 || config.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", config.Server.Port)
	}
	
	// Validate security configuration
	if len(config.Security.SessionSecret) < 32 {
		log.Printf("Warning: Session secret should be at least 32 characters long")
	}
	
	return nil
}
