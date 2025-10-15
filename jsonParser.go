package main

import (
	"encoding/json"
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

// FlexibleStringSlice can unmarshal a JSON string or array of strings into a slice of strings.
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
type Action struct {
	Tool  string                 `json:"tool"`
	Input map[string]interface{} `json:"input"`
}

// LLMResponse represents the structured JSON response from the LLM.
type LLMResponse struct {
	Version  string              `json:"version"`
	Thoughts FlexibleStringSlice `json:"thoughts"`
	Action   Action              `json:"action"`
}

func extractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)

	startIndex := strings.Index(s, "{")
	if startIndex == -1 {
		return "{}", nil
	}
	s = s[startIndex:]

	var stack []rune
	inString := false
	isEscaped := false

	for i, r := range s {
		if isEscaped {
			isEscaped = false
		} else if r == '\\' {
			isEscaped = true
		} else if r == '"' {
			inString = !inString
		}

		if !inString {
			switch r {
			case '{', '[':
				stack = append(stack, r)
			case '}' :
				if len(stack) > 0 && stack[len(stack)-1] == '{' {
					stack = stack[:len(stack)-1]
				}
			case ']':
				if len(stack) > 0 && stack[len(stack)-1] == '[' {
					stack = stack[:len(stack)-1]
				}
			}
		}

		if len(stack) == 0 && !inString && r == '}' {
			potentialJSON := s[:i+1]
			var js map[string]interface{}
			if json.Unmarshal([]byte(potentialJSON), &js) == nil {
				return potentialJSON, nil
			}
		}
	}

	// If we're here, the JSON is likely truncated. Let's try to close it.
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top == '{' {
			s += "}"
		} else if top == '[' {
			s += "]"
		}
	}
	if inString {
		s += "\""
	}

	var js map[string]interface{}
	if json.Unmarshal([]byte(s), &js) == nil {
		return s, nil
	}

	return "{}", nil
}
