// Package main implements a simple command‑line client for the
// Ollama API. The code in this file contains helper functions for
// interacting with the REST endpoints, converting model metadata, and
// handling the streaming response.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// streamChunkMsg represents a single chunk of streamed data.
// It is sent to the TUI when a new chunk arrives.
type streamChunkMsg string

// streamDoneMsg signals that the stream has finished.
// It carries the aggregated statistics and the final
// response message.
type streamDoneMsg struct {
	stats        string
	finalMessage Message
}

// errorMsg is a wrapper for errors that occur during the
// stream handling process. It can be sent to the UI as a
// tea.Msg.
type errorMsg struct{ err error }

// getModels retrieves the list of available models from the
// Ollama server by issuing a GET request to /api/tags.
func getModels(baseURL string, logger *Logger) ([]Model, error) {
	logger.Log(fmt.Sprintf("Attempting to get models from %s/api/tags", baseURL))
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		logger.Log(fmt.Sprintf("Error getting models: %v", err))
		return nil, err
	}
	defer resp.Body.Close()

	logger.Log(fmt.Sprintf("Got response status: %s", resp.Status))

	var tagsResponse TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		return nil, err
	}
	return tagsResponse.Models, nil
}

// getModelDetails fetches the detailed information for a single
// model by POSTing the model name to /api/show.
func getModelDetails(baseURL, modelName string) (*ShowModelResponse, error) {
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

	var modelResponse ShowModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResponse); err != nil {
		return nil, err
	}
	return &modelResponse, nil
}

// extractContextLength extracts the context length from a
// ModelInfo structure. Because different model architectures
// expose the context length under different field names, this
// function checks the architecture string and pulls the value
// from the appropriate field. If the architecture is
// unknown, it falls back to checking all known fields.
func extractContextLength(modelInfo *ModelInfo) int64 {
	// Determine the architecture in a case‑insensitive way.
	arch := strings.ToLower(modelInfo.Architecture)

	// Use the architecture to decide which field holds the
	// context length. This mirrors the mapping used by
	// Ollama’s own SDKs.
	switch arch {
	case "llama", "llama2", "llama3":
		return convertToInteger(modelInfo.LlamaContextLength)
	case "gemma", "gemma2", "gemma3":
		return convertToInteger(modelInfo.GemmaContextLength)
	case "mistral":
		return convertToInteger(modelInfo.MistralContextLength)
	case "gptoss": // gpt‑oss models
		return convertToInteger(modelInfo.GptossContextLength)
	default:
		// Architecture not recognised – try each known field.
		if result := convertToInteger(modelInfo.LlamaContextLength); result > 0 {
			return result
		}
		if result := convertToInteger(modelInfo.GemmaContextLength); result > 0 {
			return result
		}
		if result := convertToInteger(modelInfo.MistralContextLength); result > 0 {
			return result
		}
		if result := convertToInteger(modelInfo.GptossContextLength); result > 0 {
			return result
		}
	}

	return 0
}

// convertToInteger safely converts an arbitrary interface{} value
// into an int64. The function handles common numeric types as
// well as string representations of integers. If the conversion
// fails, it returns 0.
func convertToInteger(value interface{}) int64 {
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
