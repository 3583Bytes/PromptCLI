package agent

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"prompt-cli/internal/logger"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// Agent is responsible for executing commands received from the LLM.
type Agent struct {
	logger *logger.Logger
}

// NewAgent creates a new Agent.
func NewAgent(logger *logger.Logger) *Agent {
	return &Agent{logger: logger}
}

// ExecuteCommand processes the LLM response and executes the specified command.
func (a *Agent) ExecuteCommand(toolName string, input map[string]interface{}) string {
	if toolName == "" {
		return "" // Do nothing if the tool name is empty
	}
	switch toolName {
	case "write_file":
		return a.HandleWriteFile(input)
	case "read_file":
		return a.HandleReadFile(input)
	case "read_all_files":
		return a.HandleReadAllFiles(input)
	case "list_files":
		return a.HandleListFiles(input)
	case "delete_file":
		return a.HandleDeleteFile(input)
	case "append_file":
		return a.HandleAppendFile(input)
	case "git":
		return a.HandleGit(input)
	case "web_search":
		return a.HandleWebSearch(input)
	case "visit_url":
		return a.HandleVisitURL(input)
	case "respond":
		// This is handled by the UI, but we can log it here.
		if msg, ok := input["message"].(string); ok {
			a.logger.Log(msg)
		}
		return "" // No further action needed from the handler
	default:
		return fmt.Sprintf("Unknown command: %s", toolName)
	}
}

func (a *Agent) HandleVisitURL(input map[string]interface{}) string {
	url, ok := input["url"].(string)
	if !ok {
		return "Error: 'url' not specified or not a string for visit_url."
	}
	a.logger.Log(fmt.Sprintf("HandleVisitURL url: %s", url))

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

	text, err := ExtractTextFromHTML(res.Body)
	if err != nil {
		return fmt.Sprintf("Error extracting text from %s: %v", url, err)
	}

	if maxBytes > 0 && len(text) > int(maxBytes) {
		text = text[:int(maxBytes)]
	}

	return text
}

func (a *Agent) HandleWebSearch(input map[string]interface{}) string {
	query, ok := input["q"].(string)
	if !ok {
		return "Error: 'q' not specified or not a string for web_search."
	}
	a.logger.Log(fmt.Sprintf("HandleWebSearch query: %s", query))

	results, err := PerformWebSearch(query, a.logger)
	if err != nil {
		return fmt.Sprintf("Error performing web search: %v", err)
	}

	return results
}

func (a *Agent) HandleReadFile(input map[string]interface{}) string {
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

func (a *Agent) HandleReadAllFiles(input map[string]interface{}) string {
	glob, ok := input["glob"].(string)
	if !ok || glob == "" {
		return "Error: 'glob' pattern not specified or not a string for read_all_files."
	}

	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}

	fsys := os.DirFS(path)
	filePaths, err := doublestar.Glob(fsys, glob)
	if err != nil {
		return fmt.Sprintf("Error matching glob pattern '%s': %v", glob, err)
	}

	if len(filePaths) == 0 {
		return fmt.Sprintf("No files found matching glob pattern '%s' in directory '%s'", glob, path)
	}

	var builder strings.Builder

	for _, filePath := range filePaths {
		// doublestar.Glob returns paths relative to the fsys root, so we need to join them with the base path
		// to read the actual file from the OS.
		fullPath := filepath.Join(path, filePath)

		content, err := os.ReadFile(fullPath)
		if err != nil {
			// Log the error but continue with other files
			a.logger.Log(fmt.Sprintf("Error reading file '%s', skipping: %v", fullPath, err))
			continue
		}

		header := fmt.Sprintf("---\nFile: %s\n---\n", filePath) // Use relative path in header for clarity
		builder.WriteString(header)
		builder.Write(content)
		builder.WriteString("\n\n")
	}

	// Handle max_bytes
	maxBytes, _ := input["max_bytes"].(float64)
	output := builder.String()
	if maxBytes > 0 && len(output) > int(maxBytes) {
		output = output[:int(maxBytes)]
	}

	return output
}

func (a *Agent) HandleWriteFile(input map[string]interface{}) string {
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

	return responseToLLM
}

func (a *Agent) HandleAppendFile(input map[string]interface{}) string {
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

	return fmt.Sprintf("Content appended to file '%s' successfully.", path)
}

func (a *Agent) HandleDeleteFile(input map[string]interface{}) string {
	path, ok := input["path"].(string)
	if !ok {
		return "Error: 'path' not specified or not a string for delete_file."
	}

	err := os.Remove(path)
	if err != nil {
		return fmt.Sprintf("Error deleting file '%s': %v", path, err)
	}

	return fmt.Sprintf("File '%s' deleted successfully.", path)
}

func (a *Agent) HandleListFiles(input map[string]interface{}) string {
	a.logger.Log(fmt.Sprintf("handleListFiles input: %v", input))
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
	a.logger.Log(fmt.Sprintf("handleListFiles output: %s", result))
	return result
}

func (a *Agent) HandleGit(input map[string]interface{}) string {
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
