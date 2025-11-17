package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"prompt-cli/internal/logger"
	"prompt-cli/internal/types"
	"strconv"
	"strings"
	"time"
)

// GetModels retrieves the list of available models from the
// Ollama server by issuing a GET request to /api/tags.
func GetModels(baseURL string, logger *logger.Logger) ([]types.Model, error) {
	logger.Log(fmt.Sprintf("Attempting to get models from %s/api/tags", baseURL))

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		logger.Log(fmt.Sprintf("Error getting models: %v", err))
		return nil, err
	}
	defer resp.Body.Close()

	logger.Log(fmt.Sprintf("Got response status: %s", resp.Status))

	var tagsResponse types.TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		return nil, err
	}
	return tagsResponse.Models, nil
}

// GetModelDetails fetches the detailed information for a single
// model by POSTing the model name to /api/show.
func GetModelDetails(baseURL, modelName string) (*types.ShowModelResponse, error) {
	// Create request body
	requestBody, err := json.Marshal(map[string]string{
		"model": modelName,
	})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(baseURL+"/api/show", "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var modelResponse types.ShowModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResponse); err != nil {
		return nil, err
	}
	return &modelResponse, nil
}

// ExtractContextLength extracts the context length from a
// ModelInfo structure. Because different model architectures
// expose the context length under different field names, this
// function checks the architecture string and pulls the value
// from the appropriate field. If the architecture is
// unknown, it falls back to checking all known fields.
func ExtractContextLength(modelInfo *types.ModelInfo) int64 {
	// Determine the architecture in a case‑insensitive way.
	arch := strings.ToLower(modelInfo.Architecture)

	// Use the architecture to decide which field holds the
	// context length. This mirrors the mapping used by
	// Ollama’s own SDKs.
	switch arch {
	case "llama", "llama2", "llama3":
		return ConvertToInteger(modelInfo.LlamaContextLength)
	case "gemma", "gemma2", "gemma3":
		return ConvertToInteger(modelInfo.GemmaContextLength)
	case "mistral":
		return ConvertToInteger(modelInfo.MistralContextLength)
	case "gptoss": // gpt‑oss models
		return ConvertToInteger(modelInfo.GptossContextLength)
	default:
		// Architecture not recognised – try each known field.
		if result := ConvertToInteger(modelInfo.LlamaContextLength); result > 0 {
			return result
		}
		if result := ConvertToInteger(modelInfo.GemmaContextLength); result > 0 {
			return result
		}
		if result := ConvertToInteger(modelInfo.MistralContextLength); result > 0 {
			return result
		}
		if result := ConvertToInteger(modelInfo.GptossContextLength); result > 0 {
			return result
		}
	}

	return 0
}

// ConvertToInteger safely converts an arbitrary interface{} value
// into an int64. The function handles common numeric types as
// well as string representations of integers. If the conversion
// fails, it returns 0.
func ConvertToInteger(value interface{}) int64 {
	if value == nil {
		return 0
	}

	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		// Try to parse string as integer.
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
		return 0
	default:
		return 0
	}
}
