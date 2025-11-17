package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// --- Main Application Model ---

var footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))    // Red
var jokeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))    // Yellow

var devJokes = []string{
	"Running sudo make me a sandwich...",
	"Dividing everything by zero...",
	"Just a second... debugging reality...",
	"Thinking...or just pretending to think really hard so you don’t get bored",
	"Polishing the pixels...",
	"Debugging: Removing the needles from the haystack and then finding out you put them there.",
	"Recalibrating the humor-o-meter.",
	"Calibrating flux capacitor… please hold.",
	"Bribing the server hamsters with sunflower seeds...",
	"Decrypting the Matrix… one typo at a time.",
	"Counting to infinity… twice.",
	"Rendering the pixels’ good side...",
	"Evaluating existential crises…",
	"Consulting the sacred Stack Overflow scrolls...",
	"Loading… because everything is better when it loads.",
	"Feeding the AI some fresh data snacks…",
	"Debugging the debugging process…",
	"Waiting for the server’s coffee to kick in..",
	"Enabling quantum nonsense mode…",
	"Rearranging the alphabet for efficiency…",
	"Optimizing the unoptimized optimizer...",
	"Checking if the internet is still plugged in…",
	"Attempting to physics…",
	"Rebooting your patience…",
}

type focusable int

const (
	focusTextarea focusable = iota
	focusViewport
)

type model struct {
	viewport          viewport.Model
	textarea          textarea.Model
	messages          []Message
	apiURL            string
	modelName         string
	modelContextSize  int64 // Store context window size
	sending           bool
	error             error
	stats             string
	focused           focusable
	streaming         bool
	aiResponse        string
	stream            chan interface{}
	cancel            context.CancelFunc
	fileSearchActive  bool
	fileSearchTerm    string
	fileSearchResult  string
	files             []string
	spinner           spinner.Model
	wg                *sync.WaitGroup
	logger            *Logger
	commandHandler    *CommandHandler
	ollamaClient      *OllamaClient
	history           []string
	historyCursor     int
	ctrlCpressed      bool
	currentJoke       string
	permissionRequest *Action         // Stores the command that needs permission. If nil, not waiting.
	alwaysAllow       map[string]bool // Stores permissions for "Always Allow". Key combines toolName and relevant path.
	yoloMode          bool            // When true, bypasses all permission checks.
}

func initialModel(apiURL, modelName string, contextSize int64, systemPrompt string, logEnabled bool, logger *Logger) *model {
	// --- Text Area (Input) ---
	ta := textarea.New()
	ta.Placeholder = "Send a message... (Ctrl+V to paste)"
	ta.Focus()
	ta.Prompt = ""
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false) // Use Enter to send

	// --- Viewport (Chat History) ---
	vp := viewport.New(80, 20) // Default size, will be updated by WindowSizeMsg
	vp.KeyMap.Up.SetKeys("up")
	vp.KeyMap.Down.SetKeys("down")
	vp.KeyMap.HalfPageUp.SetEnabled(false)
	vp.KeyMap.HalfPageDown.SetEnabled(false)
	vp.KeyMap.PageUp.SetKeys("pgup")
	vp.KeyMap.PageDown.SetKeys("pgdown")
	vp.MouseWheelEnabled = true

	// --- Spinner ---
	s := spinner.New()
	s.Spinner = spinner.Line
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// --- Styles ---
	ta.FocusedStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")) // Light Blue
	ta.BlurredStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")) // Gray

	vp.Style = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")). // Purple
		Padding(0)                              // Ensure no extra padding that could cause double border effect

	// --- File list ---
	files, err := os.ReadDir(".")
	if err != nil {
		log.Println("could not list files:", err)
	}
	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}

	//For some reason Role has to be "system" have to investiget this further why.

	m := &model{
		textarea:         ta,
		viewport:         vp,
		messages:         []Message{{Role: "system", Content: systemPrompt}},
		apiURL:           apiURL,
		modelName:        modelName,
		modelContextSize: contextSize,
		sending:          false,
		stats:            "",
		focused:          focusTextarea,
		streaming:        false,
		aiResponse:       "",
		fileSearchActive: false,
		fileSearchTerm:   "",
		fileSearchResult: "",
		files:            fileNames,
		spinner:          s,
		wg:               &sync.WaitGroup{},
		logger:           logger,
		ollamaClient:     NewOllamaClient(apiURL, logger),
		history:          []string{},
		historyCursor:    -1,
		alwaysAllow:      make(map[string]bool), // Initialize the map
		yoloMode:         false,                 // Default to false
	}

	m.commandHandler = NewCommandHandler(m)
	return m
}

func (m *model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		taCmd tea.Cmd
		vpCmd tea.Cmd
	)

	// Handle permission request state first
	if m.permissionRequest != nil {
		if msg, ok := msg.(tea.KeyMsg); ok {
			var focusCmd tea.Cmd
			m.focused = focusTextarea
			focusCmd = m.textarea.Focus()

			switch strings.ToLower(msg.String()) {
			case "a": // Allow once
				action := m.permissionRequest
				m.permissionRequest = nil // Return to normal state
				model, execCmd := m.executeAndRespond(action.Tool, action.Input)
				return model, tea.Batch(focusCmd, execCmd, tea.ClearScreen)

			case "y": // Yes to all
				action := m.permissionRequest
				if path, ok := action.Input["path"].(string); ok {
					permissionKey := fmt.Sprintf("%s:%s", action.Tool, path)
					m.alwaysAllow[permissionKey] = true
				}
				m.permissionRequest = nil // Return to normal state
				model, execCmd := m.executeAndRespond(action.Tool, action.Input)
				return model, tea.Batch(focusCmd, execCmd, tea.ClearScreen)

			case "n": // No
				details := m.renderCommandDetails(m.permissionRequest)
				deniedMsg := fmt.Sprintf("Command denied by user:\n\n%s", details)
				m.messages[len(m.messages)-1].Content = deniedMsg
				m.viewport.SetContent(m.renderMessages())
				m.viewport.GotoBottom()
				m.permissionRequest = nil // Return to normal state
				return m, tea.Batch(focusCmd, tea.ClearScreen)
			}
		}
		// Ignore other messages while waiting for permission
		return m, nil
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.MouseMsg:
		m.viewport, vpCmd = m.viewport.Update(msg)
		return m, vpCmd
	case tea.KeyMsg:
		if m.ctrlCpressed {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.ctrlCpressed = false
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlY:
			m.yoloMode = !m.yoloMode
			var statusMsg string
			if m.yoloMode {
				statusMsg = "YOLO mode enabled. All commands will be executed without permission."
			} else {
				statusMsg = "YOLO mode disabled. Destructive commands will require permission."
			}
			m.messages = append(m.messages, Message{Role: "assistant", Content: statusMsg})
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()
			return m, nil
		case tea.KeyCtrlC:
			m.ctrlCpressed = true
			if m.sending {
				if m.cancel != nil {
					m.cancel()
				}
				m.streaming = false
				m.sending = false
				if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
					m.messages[len(m.messages)-1].Content += "\n\n--- Canceled ---"
				}
				m.viewport.SetContent(m.renderMessages())
				m.viewport.GotoBottom()
			}
			return m, nil
		case tea.KeyEnter:
			m.ctrlCpressed = false
			if m.focused == focusTextarea {
				return m.handleEnter()
			}
		case tea.KeyUp, tea.KeyDown:
			m.ctrlCpressed = false
			return m.handleArrowKeys(msg)
		case tea.KeyTab:
			m.ctrlCpressed = false
			return m.handleTabKey()
		case tea.KeyEsc:
			m.ctrlCpressed = false
			return m.handleEscKey()
		}

		if m.focused == focusTextarea {
			m.ctrlCpressed = false
			return m.handleTextInput(msg)
		} else {
			m.viewport, vpCmd = m.viewport.Update(msg)
		}

	case streamChunkMsg:
		if m.streaming {
			// When the first chunk arrives, clear the joke.
			if m.currentJoke != "" {
				m.currentJoke = ""
			}
			m.messages[len(m.messages)-1].Content += string(msg)
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()
			m.wg.Done()
			return m, waitForStreamCmd(m.stream)
		}

	case streamDoneMsg:
		m.wg.Wait() // Wait for all chunks to be processed
		if m.streaming {
			m.streaming = false
			m.sending = false
			m.stats = msg.stats

			finalMessage := msg.finalMessage // This is the full accumulated message
			var llmAction *Action

			// Prefer native tool_calls format
			if len(finalMessage.ToolCalls) > 0 {
				m.logger.Log("Found native tool_calls.")
				call := finalMessage.ToolCalls[0] // Assuming one call
				llmAction = &Action{
					Tool:  call.Function.Name,
					Input: call.Function.Arguments,
				}
			} else if finalMessage.Content != "" {
				// Fallback for older action-in-content format
				jsonStr, err := extractJSON(finalMessage.Content)
				if err == nil {
					var llmResponse LLMResponse
					if err := json.Unmarshal([]byte(jsonStr), &llmResponse); err == nil {
						if llmResponse.Action.Tool != "" {
							llmAction = &llmResponse.Action
						}
					}
				}
			}

			if llmAction != nil {
				// Populate the assistant's message with the tool call
				m.messages[len(m.messages)-1].ToolCalls = []ToolCall{{
					Function: FunctionCall{
						Name:      llmAction.Tool,
						Arguments: llmAction.Input,
					},
				}}
				m.messages[len(m.messages)-1].Content = "" // Clear content as ToolCalls is primary

				toolName := llmAction.Tool
				isDestructive := toolName == "write_file" || toolName == "append_file" || toolName == "delete_file"

				var permissionKey string
				if path, ok := llmAction.Input["path"].(string); ok {
					permissionKey = fmt.Sprintf("%s:%s", toolName, path)
				}

				if isDestructive && !m.alwaysAllow[permissionKey] && !m.yoloMode {
					m.permissionRequest = llmAction
					m.viewport.SetContent(m.renderMessages())
					m.viewport.GotoBottom()
					return m, nil
				}

				return m.executeAndRespond(llmAction.Tool, llmAction.Input)
			}

			// If it wasn't a tool call, the content is just plain text.
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()
		}

	case errorMsg:
		// If the error is due to context cancellation, we can ignore it,
		// as the cancellation is handled by the Ctrl-C logic.
		if strings.Contains(msg.err.Error(), "context canceled") {
			return m, nil
		}
		m.sending = false
		m.error = msg.err

	case tea.WindowSizeMsg:
		newWidth := msg.Width - 2
		textAreaRenderedHeight := m.textarea.Height() + 2 + 1
		m.textarea.SetWidth(newWidth - 2)
		m.viewport.Width = newWidth
		m.viewport.Height = msg.Height - textAreaRenderedHeight
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		m.textarea, taCmd = m.textarea.Update(msg)
		m.viewport, vpCmd = m.viewport.Update(msg)

	default:
		m.textarea, taCmd = m.textarea.Update(msg)
		m.viewport, vpCmd = m.viewport.Update(msg)
	}

	return m, tea.Batch(taCmd, vpCmd)
}

func (m *model) executeAndRespond(toolName string, input map[string]interface{}) (tea.Model, tea.Cmd) {
	if toolName == "respond" {
		m.logger.Log("Handling 'respond' tool.")
		var message string
		if msgStr, ok := input["message"].(string); ok {
			message = msgStr
		} else if msgArr, ok := input["message"].([]interface{}); ok {
			var parts []string
			for _, item := range msgArr {
				if part, ok := item.(string); ok {
					parts = append(parts, part)
				}
			}
			message = strings.Join(parts, "\n")
		}
		if message != "" {
			m.logger.Log(fmt.Sprintf("Extracted message for UI: '%.60s...'.", message))
			m.messages[len(m.messages)-1].Content = message
		}
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, nil
	}

	// Execute the command
	responseToLLM := m.commandHandler.ExecuteCommand(toolName, input)

	// Append the tool result as a "tool" message
	m.messages = append(m.messages, Message{Role: "tool", Content: responseToLLM})

	// Update the UI to show the command executed and its result
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()

	// If there was a response to send to LLM, start a new stream
	if responseToLLM != "" {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		m.sending = true
		m.streaming = true
		m.stream = make(chan interface{})
		m.messages = append(m.messages, Message{Role: "assistant", Content: ""}) // Prepare for assistant's next response
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, tea.Batch(m.ollamaClient.startStreamCmd(ctx, m.modelName, m.messages, m.modelContextSize, m.stream, m.wg), waitForStreamCmd(m.stream), m.spinner.Tick)
	}

	// If command had no response to send to LLM, just return
	return m, nil
}

func (m *model) handleArrowKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.focused != focusTextarea {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp:
		if len(m.history) > 0 {
			if m.historyCursor < len(m.history)-1 {
				m.historyCursor++
			}
			m.textarea.SetValue(m.history[m.historyCursor])
			m.textarea.CursorEnd()
		}
	case tea.KeyDown:
		if m.historyCursor > 0 {
			m.historyCursor--
			m.textarea.SetValue(m.history[m.historyCursor])
			m.textarea.CursorEnd()
		} else {
			m.historyCursor = -1
			m.textarea.Reset()
		}
	}
	return m, nil
}

func (m *model) handleTabKey() (tea.Model, tea.Cmd) {
	if m.fileSearchActive && m.fileSearchResult != "" {
		val := m.textarea.Value()
		re := regexp.MustCompile(`@\w*$`)
		newVal := re.ReplaceAllString(val, "@"+m.fileSearchResult)
		m.textarea.SetValue(newVal)
		m.fileSearchActive = false
		m.textarea.CursorEnd()
	}
	return m, nil
}

func (m *model) handleEscKey() (tea.Model, tea.Cmd) {
	if m.focused == focusTextarea {
		m.focused = focusViewport
		m.textarea.Blur()
	} else {
		m.focused = focusTextarea
		m.textarea.Focus()
	}
	return m, nil
}

func (m *model) handleTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)

	// File search logic
	val := m.textarea.Value()
	re := regexp.MustCompile(`@(\S*)$`)
	matches := re.FindStringSubmatch(val)

	if len(matches) > 1 {
		m.fileSearchActive = true
		m.fileSearchTerm = matches[1]
		m.fileSearchResult = ""
		for _, f := range m.files {
			if strings.Contains(strings.ToLower(f), strings.ToLower(m.fileSearchTerm)) {
				m.fileSearchResult = f
				break
			}
		}
	} else {
		m.fileSearchActive = false
	}
	return m, taCmd
}

func (m *model) handleEnter() (tea.Model, tea.Cmd) {
	userInput := strings.TrimSpace(m.textarea.Value())
	if userInput != "" {
		m.history = append([]string{userInput}, m.history...)
		if len(m.history) > 5 {
			m.history = m.history[:5]
		}
	}
	m.historyCursor = -1
	if userInput == "/stop" {
		if m.cancel != nil {
			m.cancel()
		}
		m.streaming = false
		m.sending = false
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
			m.messages[len(m.messages)-1].Content += "\n\n--- Canceled ---"
		}
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		m.textarea.Reset()
		return m, nil
	}

	if !m.sending {
		switch userInput {
		case "/new":
			// Preserve the system prompt, which is the first message
			if len(m.messages) > 0 {
				m.messages = []Message{m.messages[0]}
			} else {
				m.messages = []Message{}
			}
			// Clear any active streaming/sending state
			if m.cancel != nil {
				m.cancel()
			}
			m.streaming = false
			m.sending = false
			m.stats = ""
			m.currentJoke = ""

			// Update the view
			m.viewport.SetContent(m.renderMessages())
			m.textarea.Reset()
			m.viewport.GotoBottom()
			return m, nil
		case "/bye":
			return m, tea.Quit
		case "/help":
			m.messages = append(m.messages, Message{Role: "assistant", Content: "Commands:\n/new - Start a new chat session\n/bye - Exit the application\n/help - Show this help message\n/stop - Stop the current response\n/log - Toggle logging to a file\n/copy - Copy the last response to the clipboard"})
			m.viewport.SetContent(m.renderMessages())
			m.textarea.Reset()
			m.viewport.GotoBottom()
			return m, nil
		case "/copy":
			var lastResponse string
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Role == "assistant" {
					lastResponse = m.messages[i].Content
					break
				}
			}
			if lastResponse != "" {
				clipboard.WriteAll(lastResponse)
				m.messages = append(m.messages, Message{Role: "assistant", Content: "Copied last response to clipboard."})
			} else {
				m.messages = append(m.messages, Message{Role: "assistant", Content: "No response to copy."})
			}
			m.viewport.SetContent(m.renderMessages())
			m.textarea.Reset()
			m.viewport.GotoBottom()
			return m, nil
		case "/log":
			logMsg := m.logger.Toggle()
			m.messages = append(m.messages, Message{Role: "assistant", Content: logMsg})
			m.viewport.SetContent(m.renderMessages())
			m.textarea.Reset()
			m.viewport.GotoBottom()
			return m, nil
		}

		// Default action: send message
		re := regexp.MustCompile(`@(\S+)`)
		matches := re.FindAllStringSubmatch(userInput, -1)

		if len(matches) > 0 {
			processedInput := userInput
			for _, match := range matches {
				fileName := match[1]
				fileContent, err := os.ReadFile(fileName)
				if err != nil {
					continue // Keep @filename as is if file not found
				}
				replacement := fmt.Sprintf("\n\n---\nFile: %s\n```\n%s\n```\n", fileName, string(fileContent))
				processedInput = strings.Replace(processedInput, "@"+fileName, replacement, 1)
			}
			userInput = processedInput
		}

		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		m.sending = true
		m.streaming = true
		m.stream = make(chan interface{})
		m.currentJoke = devJokes[rand.Intn(len(devJokes))]
		m.logger.Log(fmt.Sprintf("User input before sending to Ollama: %s", userInput))
		m.messages = append(m.messages, Message{Role: "user", Content: userInput})
		m.messages = append(m.messages, Message{Role: "assistant", Content: ""})
		m.viewport.SetContent(m.renderMessages())

		m.textarea.Reset()
		m.viewport.GotoBottom()
		return m, tea.Batch(m.ollamaClient.startStreamCmd(ctx, m.modelName, m.messages, m.modelContextSize, m.stream, m.wg), waitForStreamCmd(m.stream), m.spinner.Tick)
	}
	return m, nil
}
func (m *model) renderMessages() string {
	// Re-create renderer with the correct width, accounting for viewport padding
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(m.viewport.Width-2),
	)

	var content strings.Builder
	for i, msg := range m.messages {
		if msg.Role == "system" {
			continue
		}

		var roleHeader string
		var renderedMsg string

		if msg.Role == "tool" {
			roleHeader = "## Tool Output"
			renderedMsg = fmt.Sprintf("```\n%s\n```", msg.Content) // Render tool output as a code block
		} else {
			roleHeader = "## " + strings.Title(msg.Role)
			if msg.IsError {
				md, _ := r.Render(fmt.Sprintf("%s\n\n%s\n\n---", roleHeader, msg.Content))
				content.WriteString(errorStyle.Render(md))
				continue
			} else {
				if msg.DisplayContent != "" {
					renderedMsg = msg.DisplayContent
				} else {
					renderedMsg = msg.Content
				}
			}
		}

		// If this is the last message, it's an assistant message, it's empty,
		// and we are waiting for a response, render the joke.
		if i == len(m.messages)-1 && msg.Role == "assistant" && msg.Content == "" && m.sending && m.currentJoke != "" {
			// Create a plain glamour renderer that only does word wrapping, no colors.
			// We subtract 2 for the padding we're adding manually.
			plainRenderer, _ := glamour.NewTermRenderer(
				glamour.WithWordWrap(m.viewport.Width - 2),
			)

			renderedJoke, _ := plainRenderer.Render(m.currentJoke)

			// 1. Style the joke content part with yellow
			yellowJoke := jokeStyle.Render(renderedJoke)

			// 2. Render the separator
			separator, _ := plainRenderer.Render("---")

			// 3. Join the parts vertically
			fullBlock := lipgloss.JoinVertical(lipgloss.Left,
				yellowJoke,
				separator,
			)

			// 4. Add left padding to the whole block for indentation
			indentedBlock := lipgloss.NewStyle().PaddingLeft(2).Render(fullBlock)

			content.WriteString(indentedBlock)
			continue
		}

		md, _ := r.Render(fmt.Sprintf("%s\n\n%s\n\n---", roleHeader, renderedMsg))
		content.WriteString(md)
	}
	return content.String()
}

func (m *model) updateFileList() {
	files, err := os.ReadDir(".")
	if err != nil {
		log.Println("could not list files:", err)
	}
	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}
	m.files = fileNames
}

// calculateUsedTokens approximates the number of tokens used in the current chat history.
// NOTE: This is a rough approximation using the heuristic of 3 words ~= 4 tokens.
// A proper implementation would require a dedicated tokenizer for the specific model.
func (m *model) calculateUsedTokens() int {
	totalWords := 0
	for _, msg := range m.messages {
		totalWords += len(strings.Fields(msg.Content))
	}
	// Approximate 3 words to 4 tokens
	return (totalWords * 4) / 3
}

// renderCommandDetails formats a command (Action) into a human-readable string for the permission prompt.
func (m *model) renderCommandDetails(action *Action) string {
	var details strings.Builder
	details.WriteString(fmt.Sprintf("Tool: %s\n", action.Tool))

	for key, value := range action.Input {
		// Don't show content for write/append, it can be long
		if (action.Tool == "write_file" || action.Tool == "append_file") && key == "content" {
			continue // Skip rendering the content field
		}

		details.WriteString(fmt.Sprintf("%s: ", strings.Title(key)))
		switch v := value.(type) {
		case string:
			details.WriteString(fmt.Sprintf("%q\n", v))
		case float64: // JSON numbers are float64 in Go
			details.WriteString(fmt.Sprintf("%v\n", v))
		case bool:
			details.WriteString(fmt.Sprintf("%t\n", v))
		default:
			details.WriteString(fmt.Sprintf("%v\n", v))
		}
	}
	return details.String()
}

func (m *model) View() string {
	if m.error != nil {
		return fmt.Sprintf("An error occurred: %v\n\nPress Ctrl+C to quit.", m.error)
	}

	// If we are waiting for permission, show the permission prompt.
	if m.permissionRequest != nil {
		m.textarea.Blur()
		m.focused = focusViewport
		details := m.renderCommandDetails(m.permissionRequest)
		prompt := fmt.Sprintf("The model wants to execute the following command:\n\n%s\nDo you want to proceed?\n\n(A)llow Once   (Y)es to All   (N)o / Display Command", details)
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewport.View(),
			lipgloss.NewStyle().Border(lipgloss.DoubleBorder(), true).BorderForeground(lipgloss.Color("1")).Padding(1).Render(prompt),
		)
	}

	if m.ctrlCpressed {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewport.View(),
			m.textarea.View(),
			footerStyle.Render("Press Ctrl-C again to exit the application. Press Esc to cancel."),
		)
	}

	var leftFooter string
	if m.fileSearchActive {
		footerText := "File search: "
		if m.fileSearchResult != "" {
			footerText += m.fileSearchResult
		} else {
			footerText += "No matches found"
		}
		leftFooter = footerStyle.Render(footerText)
	} else {
		stats := "Response time and token stats will appear here."
		if m.stats != "" {
			stats = m.stats
		}

		var contextInfo string
		if m.modelContextSize > 0 {
			usedTokens := m.calculateUsedTokens()
			remainingTokens := m.modelContextSize - int64(usedTokens)
			if remainingTokens < 0 {
				remainingTokens = 0
			}
			contextInfo = fmt.Sprintf("Context: %d | Used: %d", m.modelContextSize, usedTokens)
		} else {
			contextInfo = "Context: N/A"
		}

		var yoloIndicator string
		if m.yoloMode {
			yoloIndicator = " | YOLO"
		}

		footerText := fmt.Sprintf("Model: %s | %s | %s%s", m.modelName, contextInfo, stats, yoloIndicator)
		leftFooter = footerStyle.Render(footerText)
	}

	var rightFooter string
	if m.sending {
		rightFooter = m.spinner.View() + " Waiting for response..."
	}

	spacerWidth := m.viewport.Width - lipgloss.Width(leftFooter) - lipgloss.Width(rightFooter)
	if spacerWidth < 0 {
		spacerWidth = 0
	}
	spacer := strings.Repeat(" ", spacerWidth)

	footer := lipgloss.JoinHorizontal(lipgloss.Left,
		leftFooter,
		spacer,
		rightFooter,
	)

	if m.focused == focusViewport {
		m.viewport.Style.BorderForeground(lipgloss.Color("205")) // Orange
	} else {
		m.viewport.Style.BorderForeground(lipgloss.Color("62")) // Purple
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewport.View(),
		m.textarea.View(),
		footer,
	)
}
