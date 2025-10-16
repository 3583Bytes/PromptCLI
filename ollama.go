package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type streamChunkMsg string
type streamDoneMsg struct{ stats string; finalMessage Message }
type errorMsg struct{ err error }

func waitForStreamCmd(stream chan interface{}) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-stream
		if !ok {
			return nil
		}
		return msg
	}
}

func startStreamCmd(ctx context.Context, apiURL, modelName string, messages []Message, stream chan interface{}, wg *sync.WaitGroup) tea.Cmd {
	return func() tea.Msg {
		go func(ctx context.Context) {
			defer close(stream)

			req := ChatRequest{
				Model:    modelName,
				Messages: messages,
				Stream:   true,
			}
			reqBody, err := json.Marshal(req)
			if err != nil {
				stream <- errorMsg{err}
				return
			}

			httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL+"/api/chat", bytes.NewBuffer(reqBody))
			if err != nil {
				stream <- errorMsg{err}
				return
			}

			resp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				stream <- errorMsg{err}
				return
			}
			defer resp.Body.Close()

			startTime := time.Now()
			var finalResponse ChatResponse

			decoder := json.NewDecoder(resp.Body)
			for {
				var chatResp ChatResponse
				if err := decoder.Decode(&chatResp); err == io.EOF {
					break
				} else if err != nil {
					stream <- errorMsg{err}
					break
				}

				wg.Add(1)
				stream <- streamChunkMsg(chatResp.Message.Content)

				if chatResp.Done {
					finalResponse = chatResp
					break
				}
			}

			duration := time.Since(startTime)
			tokensPerSecond := 0.0
			if duration.Seconds() > 0 {
				tokensPerSecond = float64(finalResponse.EvalCount) / duration.Seconds()
			}
			stats := fmt.Sprintf("Time: %.2fs | Tokens/sec: %.2f", duration.Seconds(), tokensPerSecond)
			stream <- streamDoneMsg{stats: stats}
		}(ctx)
		return nil
	}
}

func getModels(baseURL string) ([]Model, error) {
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tagsResponse TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		return nil, err
	}
	return tagsResponse.Models, nil
}

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

// extractContextLength extracts the context length from the model info based on the architecture
func extractContextLength(modelInfo *ModelInfo) int64 {
	// Check architecture-specific fields based on the model architecture
	arch := strings.ToLower(modelInfo.Architecture)

	// Try different context length fields based on architecture
	switch arch {
	case "llama", "llama2", "llama3":
		return convertToInteger(modelInfo.LlamaContextLength)
	case "gemma", "gemma2", "gemma3":
		return convertToInteger(modelInfo.GemmaContextLength)
	case "mistral":
		return convertToInteger(modelInfo.MistralContextLength)
	case "gptoss": // gpt-oss models
		return convertToInteger(modelInfo.GptossContextLength)
	default:
		// Try all possible fields if architecture isn't recognized
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

// convertToInteger safely converts an interface{} to int64
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
		// Try to parse string as integer
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
		return 0
	default:
		return 0
	}
}
