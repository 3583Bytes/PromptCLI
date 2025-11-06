package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
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
func (ch *CommandHandler) ExecuteCommand(toolName string, input map[string]interface{}) string {
	if toolName == "" {
		return "" // Do nothing if the tool name is empty
	}
	switch toolName {
	case "write_file":
		return ch.handleWriteFile(input)
	case "read_file":
		return ch.handleReadFile(input)
	case "list_files":
		return ch.handleListFiles(input)
	case "delete_file":
		return ch.handleDeleteFile(input)
	case "append_file":
		return ch.handleAppendFile(input)
	case "git":
		return ch.handleGit(input)
	case "web_search":
		return ch.handleWebSearch(input)
	case "visit_url":
		return ch.handleVisitURL(input)
	case "respond":
		// This is handled by the UI, but we can log it here.
		if msg, ok := input["message"].(string); ok {
			ch.model.logger.Log(msg)
		}
		return "" // No further action needed from the handler
	default:
		return fmt.Sprintf("Unknown command: %s", toolName)
	}
}

func (ch *CommandHandler) handleVisitURL(input map[string]interface{}) string {
	url, ok := input["url"].(string)
	if !ok {
		return "Error: 'url' not specified or not a string for visit_url."
	}
	ch.model.logger.Log(fmt.Sprintf("handleVisitURL url: %s", url))

	maxBytes, _ := input["max_bytes"].(float64)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Sprintf("Error creating request for url %s: %v", url, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error fetching url %s: %v", url, err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Sprintf("Request to %s failed with status code: %d", url, res.StatusCode)
	}

	text, err := extractTextFromHTML(res.Body)
	if err != nil {
		return fmt.Sprintf("Error extracting text from %s: %v", url, err)
	}

	if maxBytes > 0 && len(text) > int(maxBytes) {
		text = text[:int(maxBytes)]
	}

	return text
}

func (ch *CommandHandler) handleWebSearch(input map[string]interface{}) string {
	query, ok := input["q"].(string)
	if !ok {
		return "Error: 'q' not specified or not a string for web_search."
	}
	ch.model.logger.Log(fmt.Sprintf("handleWebSearch query: %s", query))

	results, err := performWebSearch(query, ch.model.logger)
	if err != nil {
		return fmt.Sprintf("Error performing web search: %v", err)
	}

	return results
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

func (ch *CommandHandler) handleDeleteFile(input map[string]interface{}) string {
	path, ok := input["path"].(string)
	if !ok {
		return "Error: 'path' not specified or not a string for delete_file."
	}

	err := os.Remove(path)
	if err != nil {
		return fmt.Sprintf("Error deleting file '%s': %v", path, err)
	}

	ch.model.updateFileList()
	return fmt.Sprintf("File '%s' deleted successfully.", path)
}

func (ch *CommandHandler) handleListFiles(input map[string]interface{}) string {
	ch.model.logger.Log(fmt.Sprintf("handleListFiles input: %v", input))
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}

	glob, _ := input["glob"].(string)

	var fileNames []string
	if glob != "" {
		fsys := os.DirFS(path)
		var err error
		fileNames, err = doublestar.Glob(fsys, glob)
		if err != nil {
			return fmt.Sprintf("Error matching glob pattern '%s': %v", glob, err)
		}
	} else {
        // Original non-recursive logic if no glob is provided.
		files, err := os.ReadDir(path)
		if err != nil {
			return fmt.Sprintf("Error reading directory '%s': %v", path, err)
		}
		for _, file := range files {
			fileNames = append(fileNames, file.Name())
		}
	}

	result := fmt.Sprintf("Files in '%s':\n%s", path, strings.Join(fileNames, "\n"))
	ch.model.logger.Log(fmt.Sprintf("handleListFiles output: %s", result))
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
	} else if argsVal, ok := input["args"].(string); ok {
		args = strings.Fields(argsVal)
	} else if argsVal, ok := input["args"].(string); ok {
		args = strings.Fields(argsVal)
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
