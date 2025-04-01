package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// Config holds all configuration for the application
type Config struct {
	App      AppConfig      
	Reddit   RedditConfig   
	Database DatabaseConfig 
	Server   ServerConfig   
}

// AppConfig holds application-level configuration
type AppConfig struct {
	Name    string 
	Version string 
}

// RedditConfig holds Reddit API configuration
type RedditConfig struct {
	ClientID             string
	ClientSecret         string
	UserAgent            string
	Subreddits           []string
	PollingInterval      int
	MaxRequestsPerMinute int // value is per minute, multiply by 10 for 10-minute rate
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path string 
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port int 
}

// LoadConfig loads configuration from .env file
func LoadConfig(envPath string, log *logrus.Logger) (*Config, error) {
	if envPath == "" {
		envPath = ".env"
	}
	
	if err := godotenv.Load(envPath); err != nil {
		return nil, fmt.Errorf("failed to load .env file: %w", err)
	}
	
	subredditsStr := getEnv("REDDIT_SUBREDDITS", "golang")
	subreddits := parseSubreddits(subredditsStr)
	
	// Create config object
	config := &Config{
		App: AppConfig{
			Name:    getEnv("APP_NAME", "Reddit Tracker"),
			Version: getEnv("APP_VERSION", "1.0.0"),
		},
		Reddit: RedditConfig{
			ClientID:             getEnv("REDDIT_CLIENT_ID", ""),
			ClientSecret:         getEnv("REDDIT_CLIENT_SECRET", ""),
			UserAgent:            getEnv("REDDIT_USER_AGENT", ""),
			Subreddits:           subreddits,
			PollingInterval:      getEnvAsInt("REDDIT_POLLING_INTERVAL", 60),
			MaxRequestsPerMinute: getEnvAsInt("REDDIT_MAX_REQUESTS_PER_MINUTE", 100),
		},
		Database: DatabaseConfig{
			Path: getEnv("DATABASE_PATH", "./reddit.db"),
		},
		Server: ServerConfig{
			Port: getEnvAsInt("SERVER_PORT", 8080),
		},
	}
	
	// validation
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	
	log.WithField("file", envPath).Info("Config loaded successfully")
	return config, nil
}

// parseSubreddits parses a comma-separated list of subreddits
func parseSubreddits(subredditsStr string) []string {
	parts := strings.Split(subredditsStr, ",")
	
	subreddits := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			subreddits = append(subreddits, trimmed)
		}
	}
	
	// if no subreddits, default to "golang"
	// TODO: probably should error here instead?
	if len(subreddits) == 0 {
		subreddits = append(subreddits, "golang")
	}
	
	return subreddits
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// getEnvAsInt gets an environment variable as an integer or returns a default value
func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

// validateConfig validates the configuration
func validateConfig(config *Config) error {
	// Check Reddit API credentials
	if config.Reddit.ClientID == "" {
		return fmt.Errorf("REDDIT_CLIENT_ID environment variable is required")
	}
	if config.Reddit.ClientSecret == "" {
		return fmt.Errorf("REDDIT_CLIENT_SECRET environment variable is required")
	}
	
	// User-Agent required per API documentation;  it has strict requirements.  see example.env
	if config.Reddit.UserAgent == "" {
		return fmt.Errorf("REDDIT_USER_AGENT environment variable is required")
	}
	if len(config.Reddit.Subreddits) == 0 {
		return fmt.Errorf("REDDIT_SUBREDDITS environment variable is required")
	}
	if config.Reddit.PollingInterval < 1 {
		return fmt.Errorf("REDDIT_POLLING_INTERVAL must be positive")
	}
	
	// if we are storing the db in a nested directory, create the directory
	dbDir := filepath.Dir(config.Database.Path)
	if dbDir != "." && dbDir != "" {
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
	}
	
	return nil
} 