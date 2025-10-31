package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"prompt-cli/config"

	tea "github.com/charmbracelet/bubbletea"
)

// loadPrompt reads the contents of the prompt file located at the given path.
// It returns the file content as a string and any error that occurs.
func loadPrompt(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func main() {
	// Determine the directory of the running executable.
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Error finding executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)

	// Build the path to the configuration file relative to the executable directory.
	configPath := filepath.Join(exeDir, "config.json")

	// Load configuration from the JSON file.
	configs, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config from %s: %v", configPath, err)
	}

	// Validate the loaded configuration to ensure required fields are set.
	if err := config.ValidateConfig(configs); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	logger := NewLogger()
	if configs.LogEnabled {
		logger.Toggle()
	}
	logger.Setup()

	// Construct the base URL for the Ollama server, adding the HTTP scheme if missing.
	var baseURL string
	if strings.HasPrefix(configs.OllamaServerURL, "http") {
		baseURL = fmt.Sprintf("%s:%d", configs.OllamaServerURL, configs.OllamaServerPort)
	} else {
		baseURL = fmt.Sprintf("http://%s:%d", configs.OllamaServerURL, configs.OllamaServerPort)
	}

	logger.Log(fmt.Sprintf("Connecting to Ollama at: %s", baseURL))

	// Retrieve the list of available models from the Ollama server.
	models, err := getModels(baseURL, logger)
	if err != nil {
		log.Fatalf("Error getting models: %v", err)
	}
	if len(models) == 0 {
		log.Fatal("No models found on the Ollama server.")
	}

	// Determine which model to use: a default from config or user selection.
	var selectedModel string
	if configs.DefaultLLM != "" {
		selectedModel = configs.DefaultLLM
	} else {
		fmt.Println("Please select a model:")
		for i, m := range models {
			fmt.Printf("%d: %s\n", i+1, m.Name)
		}

		// Prompt the user until a valid model index is entered.
		var choice int
		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("> ")
			input, _ := reader.ReadString('\n')
			choice, err = strconv.Atoi(strings.TrimSpace(input))
			if err == nil && choice > 0 && choice <= len(models) {
				break
			}
			fmt.Println("Invalid choice, please try again.")
		}
		selectedModel = models[choice-1].Name
	}

	// Fetch model details to determine the context window size.
	modelDetails, err := getModelDetails(baseURL, selectedModel)
	var contextSize int64 = 0 // Default to 0 if details are unavailable.
	if err != nil {
		log.Printf("Warning: Could not get model details for %s: %v", selectedModel, err)
	} else if modelDetails != nil {
		contextSize = extractContextLength(&modelDetails.ModelInfo)
	}

	// Load the system prompt from a Markdown file; fall back to a default prompt if missing.
	systemPrompt, err := loadPrompt("Prompt.MD")
	if err != nil {
		log.Printf("Warning: Could not load system prompt: %v", err)
		systemPrompt = "You are a helpful assistant."
	}

	// Initialize the Bubble Tea model with the gathered configuration.
	m := initialModel(baseURL, selectedModel, contextSize, systemPrompt, configs.LogEnabled, logger)

	// Create a new Bubble Tea program with alternate screen and mouse support.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())

	// Run the TUI; terminate on error.
	if _, err := p.Run(); err != nil {
		log.Fatalf("Alas, there's been an error: %v", err)
	}
}
