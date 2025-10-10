package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)


// CommandHandler is responsible for executing commands received from the LLM.
type CommandHandler struct {
	model *model
}

// NewCommandHandler creates a new CommandHandler.
func NewCommandHandler(m *model) *CommandHandler {
	return &CommandHandler{model: m}
}

// ExecuteCommand processes the LLM response and executes the specified command.
func (ch *CommandHandler) ExecuteCommand(llmResponse *LLMResponse) string {
	switch llmResponse.Action.Tool {
	case "write_file":
		return ch.handleWriteFile(llmResponse.Action.Input)
	case "read_file":
		return ch.handleReadFile(llmResponse.Action.Input)
	case "list_files":
		return ch.handleListFiles(llmResponse.Action.Input)
	case "delete_file":
		// To be implemented
		return "delete_file command not implemented yet."
	case "append_file":
		return ch.handleAppendFile(llmResponse.Action.Input)
	case "git":
		return ch.handleGit(llmResponse.Action.Input)
	case "respond":
		// This is handled by the UI, but we can log it here.
		ch.model.logToFile(fmt.Sprintf("LLM responded: %v", llmResponse.Action.Input["message"]))
		return "" // No further action needed from the handler
	default:
		return fmt.Sprintf("Unknown command: %s", llmResponse.Action.Tool)
	}
}

func (ch *CommandHandler) handleReadFile(input map[string]interface{}) string {
	path, ok := input["path"].(string)
	if !ok {
		return "Error: 'path' not specified or not a string for read_file."
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Error reading file '%s': %v", path, err)
	}

	return string(content)
}

func (ch *CommandHandler) handleWriteFile(input map[string]interface{}) string {
	path, ok := input["path"].(string)
	if !ok {
		return "Error: 'path' not specified or not a string for write_file."
	}
	content, ok := input["content"].(string)
	if !ok {
		return "Error: 'content' not specified or not a string for write_file."
	}
	mode, _ := input["mode"].(string) // Default is effectively "overwrite" if not specified

	var responseToLLM string

	if mode == "create_only" {
		_, err := os.Stat(path)
		if err == nil {
			responseToLLM = fmt.Sprintf("File '%s' already exists.", path)
		} else {
			err := os.WriteFile(path, []byte(content), 0644)
			if err != nil {
				responseToLLM = fmt.Sprintf("Error creating file '%s': %v", path, err)
			} else {
				responseToLLM = fmt.Sprintf("File '%s' created successfully.", path)
			}
		}
	} else { // "overwrite" is the default
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			responseToLLM = fmt.Sprintf("Error writing to file '%s': %v", path, err)
		} else {
			responseToLLM = fmt.Sprintf("File '%s' overwritten successfully.", path)
		}
	}

	ch.model.updateFileList()
	return responseToLLM
}

func (ch *CommandHandler) handleAppendFile(input map[string]interface{}) string {
	path, ok := input["path"].(string)
	if !ok {
		return "Error: 'path' not specified or not a string for append_file."
	}
	content, ok := input["content"].(string)
	if !ok {
		return "Error: 'content' not specified or not a string for append_file."
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Sprintf("Error opening file '%s': %v", path, err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Sprintf("Error appending to file '%s': %v", path, err)
	}

	ch.model.updateFileList()
	return fmt.Sprintf("Content appended to file '%s' successfully.", path)
}

func (ch *CommandHandler) handleListFiles(input map[string]interface{}) string {
	ch.model.logToFile(fmt.Sprintf("handleListFiles input: %v", input))
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}

	files, err := os.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("Error reading directory '%s': %v", path, err)
	}

	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}

	glob, _ := input["glob"].(string)
	if glob != "" {
		var matchedFiles []string
		for _, fileName := range fileNames {
			matched, err := filepath.Match(glob, fileName)
			if err != nil {
				return fmt.Sprintf("Error matching glob pattern: %v", err)
			}
			if matched {
				matchedFiles = append(matchedFiles, fileName)
			}
		}
		fileNames = matchedFiles
	}

	result := fmt.Sprintf("Files in '%s':\n%s", path, strings.Join(fileNames, "\n"))
	ch.model.logToFile(fmt.Sprintf("handleListFiles output: %s", result))
	return result
}

func (ch *CommandHandler) handleGit(input map[string]interface{}) string {
	cmd, ok := input["cmd"].(string)
	if !ok {
		return "Error: 'cmd' not specified or not a string for git."
	}

	var args []string
	if argsVal, ok := input["args"].([]interface{}); ok {
		for _, arg := range argsVal {
			if argStr, ok := arg.(string); ok {
				args = append(args, argStr)
			}
		}
	}

	cwd, _ := input["cwd"].(string)
	timeout_ms, _ := input["timeout_ms"].(float64)
	max_bytes, _ := input["max_bytes"].(float64)

	if timeout_ms == 0 {
		timeout_ms = 5000 // default timeout of 5 seconds
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout_ms)*time.Millisecond)
	defer cancel()

	command := exec.CommandContext(ctx, "git", append([]string{cmd}, args...)...)
	command.Dir = cwd

	var out bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &out
	command.Stderr = &stderr

	err := command.Run()

	if err != nil {
		return fmt.Sprintf("Error executing git command: %v\nStderr: %s", err, stderr.String())
	}

	output := out.String()
	if max_bytes > 0 && len(output) > int(max_bytes) {
		output = output[:int(max_bytes)]
	}

	return output
}