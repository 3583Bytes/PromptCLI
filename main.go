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

func loadPrompt(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func main() {
	// Load configuration from executable's directory
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Error finding executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.json")
	configs, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config from %s: %v", configPath, err)
	}

	// Validate the configuration
	if err := config.ValidateConfig(configs); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	baseURL := fmt.Sprintf("%s:%d", configs.OllamaServerURL, configs.OllamaServerPort)

	models, err := getModels(baseURL)
	if err != nil {
		log.Fatalf("Error getting models: %v", err)
	}
	if len(models) == 0 {
		log.Fatal("No models found on the Ollama server.")
	}

	var selectedModel string
	if configs.DefaultLLM != "" {
		selectedModel = configs.DefaultLLM
	} else {
		fmt.Println("Please select a model:")
		for i, m := range models {
			fmt.Printf("%d: %s\n", i+1, m.Name)
		}

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

	// Get model details to retrieve context window size
	modelDetails, err := getModelDetails(baseURL, selectedModel)
	var contextSize int64 = 0 // Default to 0 if not available
	if err != nil {
		log.Printf("Warning: Could not get model details for %s: %v", selectedModel, err)
	} else if modelDetails != nil {
		contextSize = extractContextLength(&modelDetails.ModelInfo)
	}

	// Start the Bubble Tea program
	systemPrompt, err := loadPrompt("Prompt.MD")
	if err != nil {
		log.Printf("Warning: Could not load system prompt: %v", err)
		systemPrompt = "You are a helpful assistant."
	}

	m := initialModel(baseURL, selectedModel, contextSize, systemPrompt, configs.LogEnabled)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())

	if _, err := p.Run(); err != nil {
		log.Fatalf("Alas, there's been an error: %v", err)
	}
}