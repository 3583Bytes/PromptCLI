package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"prompt-cli/internal/logger"
	"prompt-cli/internal/types"
	"sync"
	"time"


)

// OllamaClient is responsible for communicating with the Ollama API.
type OllamaClient struct {
	apiURL string
	logger *logger.Logger
}

// NewOllamaClient creates a new OllamaClient.
func NewOllamaClient(apiURL string, logger *logger.Logger) *OllamaClient {
	return &OllamaClient{
		apiURL: apiURL,
		logger: logger,
	}
}



func (c *OllamaClient) StartStream(ctx context.Context, modelName string, messages []types.Message, contextLength int64, stream chan interface{}, wg *sync.WaitGroup) {
	go func() {
		defer close(stream)

		req := types.ChatRequest{
			Model:    modelName,
			Messages: messages,
			Stream:   true,
			Options:  types.Options{NumCtx: contextLength},
		}
		reqBody, err := json.Marshal(req)
		if err != nil {
			stream <- types.ErrorMsg{Err: err}
			return
		}

		c.logger.Log(fmt.Sprintf("Sending request to Ollama: %s", string(reqBody)))

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+"/api/chat", bytes.NewBuffer(reqBody))
		if err != nil {
			c.logger.Log(fmt.Sprintf("Error creating request: %v", err))
			stream <- types.ErrorMsg{Err: err}
			return
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			c.logger.Log(fmt.Sprintf("Error sending request: %v", err))
			stream <- types.ErrorMsg{Err: err}
			return
		}
		defer resp.Body.Close()

		c.logger.Log(fmt.Sprintf("Ollama response status: %s", resp.Status))

		startTime := time.Now()
		var finalResponse types.ChatResponse // For stats at the end
		var accumulatedMessage types.Message    // Accumulate the full message here

		decoder := json.NewDecoder(resp.Body)
		for {
			var chatResp types.ChatResponse
			if err := decoder.Decode(&chatResp); err == io.EOF {
				break
			} else if err != nil {
				stream <- types.ErrorMsg{Err: fmt.Errorf("error decoding stream chunk: %v", err)}
				break
			}

			// Send content chunk for live display
			if chatResp.Message.Content != "" {
				wg.Add(1)
				stream <- types.StreamChunkMsg(chatResp.Message.Content)
			}

			// Accumulate the complete message object
			if accumulatedMessage.Role == "" {
				accumulatedMessage.Role = chatResp.Message.Role
			}
			accumulatedMessage.Content += chatResp.Message.Content
			if len(chatResp.Message.ToolCalls) > 0 {
				accumulatedMessage.ToolCalls = append(accumulatedMessage.ToolCalls, chatResp.Message.ToolCalls...)
			}

			if chatResp.Done {
				c.logger.Log("Received 'Done: true' from Ollama API.")
				c.logger.Log(fmt.Sprintf("Final accumulated message content before parsing: %s", accumulatedMessage.Content))
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
		stream <- types.StreamDoneMsg{Stats: stats, FinalMessage: accumulatedMessage} // Send the *accumulated* message
	}()
}