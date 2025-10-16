package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds the application configuration
type Config struct {
	OllamaServerURL  string `json:"ollama_server_url"`
	OllamaServerPort int    `json:"ollama_server_port"`
	DefaultLLM       string `json:"default_llm"`
	LogEnabled       bool   `json:"log_enabled,omitempty"`
}

// LoadConfig loads the configuration from the specified file path
// It returns a populated Config struct or an error if the file cannot be read or parsed
func LoadConfig(path string) (*Config, error) {
	// Check if the config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file does not exist: %s", path)
	}

	// Open the config file
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	// Create a new decoder
	decoder := json.NewDecoder(file)
	config := &Config{}

	// Decode the JSON into the config struct
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}

	// Set default values if not specified
	if config.OllamaServerURL == "" {
		config.OllamaServerURL = "http://localhost"
	}
	if config.OllamaServerPort == 0 {
		config.OllamaServerPort = 11434 // Default Ollama port
	}

	return config, nil
}

// ValidateConfig checks that the configuration is valid
func ValidateConfig(config *Config) error {
	if config.OllamaServerURL == "" {
		return fmt.Errorf("ollama server URL cannot be empty")
	}
	if config.OllamaServerPort <= 0 {
		return fmt.Errorf("ollama server port must be greater than 0")
	}
	return nil
}