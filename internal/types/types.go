// jsonParser.go
// This file defines the data structures and helper functions
// used by the CLI to communicate with the Ollama API.  It
// contains the JSON schemas for model listings, model
// details, chat requests/responses, and a utility for
// extracting JSON from noisy output.

package types

import (
	"encoding/json"
	"regexp"
	"strings"
)

// --- API Data Structures ---
// TagsResponse represents the response from the /tags endpoint.
// It contains a list of Model objects.
//
// The rest of the structs mirror the JSON returned by the
// /show/<model> endpoint and the chat API.

// TagsResponse holds the list of available models.
type TagsResponse struct {
	Models []Model `json:"models"`
}

// Model represents a single model entry in the tags list.
type Model struct {
	Name string `json:"name"`
}

// ShowModelResponse contains detailed information about a model.
// The Details field holds generic metadata and ModelInfo holds
// architecture‑specific fields.
type ShowModelResponse struct {
	Details   Details   `json:"details"`
	ModelInfo ModelInfo `json:"model_info"`
}

// Details describes the general model metadata.
type Details struct {
	Format            string `json:"format"`
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

// ModelInfo contains architecture‑specific fields.  The
// GenericContextLength field is not marshalled but is set
// dynamically at runtime based on the architecture.
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

// Options represents the options for a chat request.
type Options struct {
	NumCtx int64 `json:"num_ctx,omitempty"`
}

// ChatRequest represents a request to the chat endpoint.
// It contains the model, a sequence of messages, and a flag
// indicating whether the response should be streamed.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Options  Options   `json:"options,omitempty"`
}

// Message is an individual chat message.  It may contain tool
// calls and an error flag used internally.
type Message struct {
	Role           string     `json:"role"`
	Content        string     `json:"content"`
	DisplayContent string     `json:"-"`
	ToolCalls      []ToolCall `json:"tool_calls,omitempty"`
	IsError        bool       `json:"-"`
}

// ChatResponse is the response from the chat endpoint.
// It contains the resulting message, a done flag, and
// the number of evaluation tokens.
type ChatResponse struct {
	Message   Message `json:"message"`
	Done      bool    `json:"done"`
	EvalCount int     `json:"eval_count"`
}

// FlexibleStringSlice can unmarshal a JSON string or array of strings
// into a slice of strings.  This is useful for optional fields that
// may appear as either a single string or a list.
type FlexibleStringSlice []string

func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		*f = []string{str}
		return nil
	}

	var s []string
	if err := json.Unmarshal(data, &s); err != nil {
		// If it's not a valid array, we'll just ignore it.
		*f = []string{}
		return nil
	}
	*f = s
	return nil
}

// Action represents the action to be taken by the tool.
// It is sent to the LLM to request execution.
type Action struct {
	Tool  string                 `json:"tool"`
	Input map[string]interface{} `json:"input"`
}

// ToolCall represents a single tool call from the LLM.
type ToolCall struct {
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the function to be called.
type FunctionCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// LLMResponse represents the structured JSON response from the LLM.
// It contains the action the LLM wants to perform and any tool calls.
type LLMResponse struct {
	Version   string              `json:"version"`
	Thoughts  FlexibleStringSlice `json:"thoughts"`
	Action    Action              `json:"action"`
	ToolCalls []ToolCall          `json:"tool_calls"`
}

// StreamChunkMsg represents a single chunk of streamed data.
type StreamChunkMsg string

// StreamDoneMsg signals that the stream has finished.
type StreamDoneMsg struct {
	Stats        string
	FinalMessage Message
}

// ErrorMsg is a wrapper for errors that occur during the
// stream handling process.
type ErrorMsg struct{ Err error }

// fixGitArgs normalises the "args" field in a git JSON payload so
// that each argument is a separate string.  This is required by the
// underlying command line interface.
func FixGitArgs(jsonStr string) string {
	// Case 1: "args": [-n 1]
	re1 := regexp.MustCompile(`"args":\s*\[([^\]]*)\]`)
	jsonStr = re1.ReplaceAllStringFunc(jsonStr, func(match string) string {
		contentMatch := re1.FindStringSubmatch(match)
		if len(contentMatch) < 2 {
			return match
		}
		content := contentMatch[1]

		if strings.Contains(content, `"`) || strings.TrimSpace(content) == "" {
			return match
		}

		parts := strings.Fields(content)
		var newParts []string
		for _, p := range parts {
			escapedPart := strings.ReplaceAll(p, `"`, `\"`)
			newParts = append(newParts, `"`+escapedPart+`"`)
		}
		return `"args": [` + strings.Join(newParts, ", ") + `]`
	})

	// Case 2: "args": "-n 1"
	re2 := regexp.MustCompile(`"args":\s*"([^"]*)"`) // Corrected regex for case 2
	jsonStr = re2.ReplaceAllStringFunc(jsonStr, func(match string) string {
		contentMatch := re2.FindStringSubmatch(match)
		if len(contentMatch) < 2 {
			return match
		}
		content := contentMatch[1]

		parts := strings.Fields(content)
		var newParts []string
		for _, p := range parts {
			escapedPart := strings.ReplaceAll(p, `"`, `\"`)
			newParts = append(newParts, `"`+escapedPart+`"`)
		}
		return `"args": [` + strings.Join(newParts, ", ") + `]`
	})

	return jsonStr
}

// extractJSON attempts to extract a valid JSON object from a string that may
// contain noise or partial data. It is used to parse output from the chat
// endpoint. This implementation is designed to be robust against common LLM
// errors, such as mismatched brackets and trailing commas.
func ExtractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)

	startIndex := strings.Index(s, "{")
	if startIndex == -1 {
		return "{}", nil
	}
	s = s[startIndex:]

	var result strings.Builder
	var stack []rune
	inString := false
	isEscaped := false

	// Regex to find and remove trailing commas
	reTrailingComma := regexp.MustCompile(`,(\s*[\}\]])`)

	for _, r := range s {
		// Always write the rune to the builder unless it's a mismatched closer.
		// We handle string state first.
		if isEscaped {
			isEscaped = false
			result.WriteRune(r)
			continue
		}
		if r == '\\' {
			isEscaped = true
			result.WriteRune(r)
			continue
		}
		if r == '"' {
			inString = !inString
		}

		if !inString {
			switch r {
			case '{', '[':
				stack = append(stack, r)
			case '}':
				if len(stack) > 0 && stack[len(stack)-1] == '{' {
					stack = stack[:len(stack)-1]
				} else {
					continue // Skip mismatched '}'
				}
			case ']':
				if len(stack) > 0 && stack[len(stack)-1] == '[' {
					stack = stack[:len(stack)-1]
				} else {
					continue // Skip mismatched ']'
				}
			}
		}

		result.WriteRune(r)

		// If the stack is empty and we've just processed a '}', we might have a complete object.
		if len(stack) == 0 && !inString && r == '}' {
			potentialJSON := result.String()
			cleanedJSON := reTrailingComma.ReplaceAllString(potentialJSON, "$1")

			var js map[string]interface{}
			if json.Unmarshal([]byte(cleanedJSON), &js) == nil {
				// It's a valid JSON object. Return it.
				return cleanedJSON, nil
			}
		}
	}

	// If we've reached the end of the string, the JSON might be truncated.
	// Let's try to close any open structures.
	finalJSONStr := result.String()
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top == '{' {
			finalJSONStr += "}"
		} else if top == '[' {
			finalJSONStr += "]"
		}
	}
	if inString {
		finalJSONStr += `"`
	}

	// Clean up any trailing commas in the potentially fixed string.
	cleanedJSON := reTrailingComma.ReplaceAllString(finalJSONStr, "$1")
	var js map[string]interface{}
	if json.Unmarshal([]byte(cleanedJSON), &js) == nil {
		return cleanedJSON, nil
	}

	// If all attempts to parse fail, return an empty JSON object.
	return "{}", nil
}