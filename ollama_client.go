package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// OllamaClient is responsible for communicating with the Ollama API.
type OllamaClient struct {
	apiURL string
	logger *Logger
}

// NewOllamaClient creates a new OllamaClient.
func NewOllamaClient(apiURL string, logger *Logger) *OllamaClient {
	return &OllamaClient{
		apiURL: apiURL,
		logger: logger,
	}
}

func (c *OllamaClient) startStreamCmd(ctx context.Context, modelName string, messages []Message, stream chan interface{}, wg *sync.WaitGroup) tea.Cmd {
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

			c.logger.Log(fmt.Sprintf("Sending request to Ollama: %s", string(reqBody)))

			httpReq, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+"/api/chat", bytes.NewBuffer(reqBody))
			if err != nil {
				c.logger.Log(fmt.Sprintf("Error creating request: %v", err))
				stream <- errorMsg{err}
				return
			}

			resp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				c.logger.Log(fmt.Sprintf("Error sending request: %v", err))
				stream <- errorMsg{err}
				return
			}
			defer resp.Body.Close()

			c.logger.Log(fmt.Sprintf("Ollama response status: %s", resp.Status))

			startTime := time.Now()
			var finalResponse ChatResponse // For stats at the end
			var accumulatedMessage Message    // Accumulate the full message here

			decoder := json.NewDecoder(resp.Body)
			for {
				var chatResp ChatResponse
				if err := decoder.Decode(&chatResp); err == io.EOF {
					break
				} else if err != nil {
					stream <- errorMsg{fmt.Errorf("error decoding stream chunk: %v", err)}
					break
				}

				// Send content chunk for live display
				if chatResp.Message.Content != "" {
					wg.Add(1)
					stream <- chatResp.Message.Content
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
			stream <- streamDoneMsg{stats: stats, finalMessage: accumulatedMessage} // Send the *accumulated* message
		}(ctx)
		return nil
	}
}

func waitForStreamCmd(stream chan interface{}) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-stream
		if !ok {
			return nil
		}
		switch msg := msg.(type) {
		case string:
			return streamChunkMsg(msg)
		case streamDoneMsg:
			return msg
		case errorMsg:
			return msg // Pass the error message through
		default:
			return errorMsg{fmt.Errorf("unknown message type: %T", msg)}
		}
	}
}