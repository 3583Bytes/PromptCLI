package main

import (
	"fmt"
	"strings"
)

// --- API Data Structures ---
type TagsResponse struct {
	Models []Model `json:"models"`
}
type Model struct {
	Name string `json:"name"`
}
type ShowModelResponse struct {
	Details   Details   `json:"details"`
	ModelInfo ModelInfo `json:"model_info"`
}
type Details struct {
	Format            string `json:"format"`
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}
type ModelInfo struct {
	// Generic field that can capture context length regardless of model architecture
	GenericContextLength interface{} `json:"-"` // This will be set dynamically based on architecture
	BlockCount           interface{} `json:"llama.block_count,omitempty"`
	EmbeddingLength      interface{} `json:"llama.embedding_length,omitempty"`
	VocabSize            interface{} `json:"llama.vocab_size,omitempty"`
	ParameterCount       int64       `json:"general.parameter_count,omitempty"`
	Architecture         string      `json:"general.architecture,omitempty"`
	// Architecture-specific fields
	LlamaContextLength interface{} `json:"llama.context_length,omitempty"`
	GemmaContextLength interface{} `json:"gemma.context_length,omitempty"`

	MistralContextLength interface{} `json:"mistral.context_length,omitempty"`
	GptossContextLength  interface{} `json:"gptoss.context_length,omitempty"`
}
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	IsError bool   `json:"-"`
}
type ChatResponse struct {
	Message   Message `json:"message"`
	Done      bool    `json:"done"`
	EvalCount int     `json:"eval_count"`
}

// LLMResponse represents the structured JSON response from the LLM.
type LLMResponse struct {
	Version  string   `json:"version"`
	Thoughts []string `json:"thoughts"`
	Action   struct {
		Tool  string                 `json:"tool"`
		Input map[string]interface{} `json:"input"`
	} `json:"action"`
}

func sanitizeJSON(jsonStr string) string {
	verbatimStringMarker := "\"content\": @\""
	startIndex := strings.Index(jsonStr, verbatimStringMarker)
	if startIndex == -1 {
		return jsonStr // No verbatim string found
	}

	// The verbatim string starts after the marker
	contentStartIndex := startIndex + len(verbatimStringMarker)

	// Find the closing quote of the verbatim string.
	// It's the last quote in the string, because we assume the content is the last field.
	endIndex := strings.LastIndex(jsonStr, "\"")
	if endIndex <= contentStartIndex {
		return jsonStr // Something is wrong
	}

	rawContent := jsonStr[contentStartIndex:endIndex]

	// Escape the content for JSON
	var escapedContent strings.Builder
	for _, r := range rawContent {
		switch r {
		case '\\':
			escapedContent.WriteString("\\")
		case '"':
			escapedContent.WriteString("\"")
		case '\n':
			escapedContent.WriteString("\\n")
		case '\r':
			escapedContent.WriteString("\\r")
		case '\t':
			escapedContent.WriteString("\\t")
		default:
			escapedContent.WriteRune(r)
		}
	}

	// Reconstruct the JSON
	prefix := jsonStr[:startIndex+len("\"content\": ")]
	suffix := jsonStr[endIndex+1:]

	return prefix + "\"" + escapedContent.String() + "\"" + suffix
}

func fixTruncatedJSON(jsonStr string) string {
	var stack []rune
	inString := false

	for i := 0; i < len(jsonStr); i++ {
		char := rune(jsonStr[i])

		if char == '"' {
			// Check for preceding backslashes to determine if the quote is escaped
			isEscaped := false
			j := i - 1
			for j >= 0 && jsonStr[j] == '\\' {
				isEscaped = !isEscaped
				j--
			}
			if !isEscaped {
				inString = !inString
			}
		}

		if inString {
			continue
		}

		switch char {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '}' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == ']' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// Append the missing closing characters in reverse order
	for i := len(stack) - 1; i >= 0; i-- {
		jsonStr += string(stack[i])
	}

	return jsonStr
}

func extractJSON(s string) (string, error) {
	// Find the start of the JSON block
	startMarker := "```json"
	startIndex := strings.Index(s, startMarker)

	var jsonContent string

	if startIndex != -1 {
		// Found ```json marker
		startIndex += len(startMarker)

		// Find the end of the JSON block
		endMarker := "```"
		endIndex := strings.LastIndex(s, endMarker)
		if endIndex == -1 || endIndex <= startIndex {
			return "", fmt.Errorf("could not find closing marker for JSON block")
		}
		jsonContent = s[startIndex:endIndex]
	} else {
		// If no ```json marker, look for a raw JSON object
		startIndex := strings.Index(s, "{")
		if startIndex == -1 {
			return "", fmt.Errorf("no JSON object found in the response")
		}

		endIndex := strings.LastIndex(s, "}")
		if endIndex == -1 || endIndex < startIndex {
			return "", fmt.Errorf("invalid JSON object found in the response")
		}
		jsonContent = s[startIndex : endIndex+1]
	}

	return strings.TrimSpace(jsonContent), nil
}
