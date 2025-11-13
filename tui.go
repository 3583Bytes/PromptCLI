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
	"Why do programmers prefer dark mode? Because light attracts bugs.",
	"Why did the programmer quit his job? Because he didn't get arrays.",
	"What's the object-oriented way to become wealthy? Inheritance.",
	"Why do Java developers wear glasses? Because they don't C#.",
	"A programmer puts two glasses on his bedside table. One with water if he gets thirsty, and one empty in case he doesn't.",
	"Debugging: Removing the needles from the haystack and then finding out you put them there.",
	"Why was the JavaScript developer sad? Because he didn't Node how to Express himself.",
	"There are 10 types of people in the world: those who understand binary, and those who don't.",
	"What's a programmer's favorite place to hang out? Foo Bar.",
	"Why do programmers always mix up Halloween and Christmas? Because Oct 31 == Dec 25.",
	"How many programmers does it take to change a light bulb? None, that's a hardware problem.",
	"What's the best thing about a boolean? Even if you're wrong, you're only off by a bit.",
	"Why do C++ programmers prefer to use the old C-style casts? Because they're tired of `dynamic_cast` failing at runtime.",
	"What do you call a programmer who can't code? A debugger.",
	"My code doesn't have bugs, it has unexpected features.",
}

type focusable int

const (
	focusTextarea focusable = iota
	focusViewport
)

type model struct {
	viewport         viewport.Model
	textarea         textarea.Model
	messages         []Message
	apiURL           string
	modelName        string
	modelContextSize int64 // Store context window size
	sending          bool
	error            error
	stats            string
	focused          focusable
	streaming        bool
	aiResponse       string
	stream           chan interface{}
	cancel           context.CancelFunc
	fileSearchActive bool
	fileSearchTerm   string
	fileSearchResult string
	files            []string
	spinner          spinner.Model
	wg               *sync.WaitGroup
	logger           *Logger
	commandHandler   *CommandHandler
	ollamaClient     *OllamaClient
	history          []string
	historyCursor    int
	ctrlCpressed     bool
	currentJoke      string
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
			var toolName string
			var input map[string]interface{}
			var isToolCall bool = false

			// Prefer native tool_calls format
			if len(finalMessage.ToolCalls) > 0 {
				m.logger.Log("Found native tool_calls.")
				isToolCall = true
				call := finalMessage.ToolCalls[0] // Assuming one call
				toolName = call.Function.Name
				input = call.Function.Arguments
			} else if finalMessage.Content != "" {
				// Fallback for older action-in-content format
				jsonStr, err := extractJSON(finalMessage.Content)
				if err != nil {
					m.logger.Log(fmt.Sprintf("Could not extract JSON: %v", err))
				} else {
					var llmResponse LLMResponse
					if err := json.Unmarshal([]byte(jsonStr), &llmResponse); err == nil {
						m.logger.Log(fmt.Sprintf("Unmarshaled LLMResponse: %+v", llmResponse))
						if llmResponse.Action.Tool != "" {
							m.logger.Log(fmt.Sprintf("Successfully parsed action-in-content JSON. Tool: '%s'", llmResponse.Action.Tool))
							isToolCall = true
							toolName = llmResponse.Action.Tool
							input = llmResponse.Action.Input
						} else {
							m.logger.Log("Parsed JSON, but tool name is empty. Not a tool call.")
						}
					} else {
						m.logger.Log(fmt.Sprintf("Failed to unmarshal JSON from extractJSON: %v", err))
					}
				}
			}

			if isToolCall {
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
				} else {
					// Format "Command Received" and execute
					m.messages[len(m.messages)-1].Content = fmt.Sprintf("**Command Executed**: `%s`", toolName)

					if responseToLLM := m.commandHandler.ExecuteCommand(toolName, input); responseToLLM != "" {
						// Find the last user message to provide context to the LLM.
						var lastUserMessage string
						for i := len(m.messages) - 1; i >= 0; i-- {
							if m.messages[i].Role == "user" {
								lastUserMessage = m.messages[i].Content
								break
							}
						}

						// Construct the new message with the original user message and the tool results.
						newContent := fmt.Sprintf("%s\n\nTool results for `%s`:\n%s", lastUserMessage, toolName, responseToLLM)

						// ... start new stream ...
						m.logger.Log(fmt.Sprintf("Response to LLM: %s", newContent))
						m.messages = append(m.messages, Message{Role: "user", Content: newContent, DisplayContent: fmt.Sprintf("Tool results for `%s` sent to model.", toolName)})
						ctx, cancel := context.WithCancel(context.Background())
						m.cancel = cancel
						m.sending = true
						m.streaming = true
						m.stream = make(chan interface{})
						m.messages = append(m.messages, Message{Role: "assistant", Content: ""})
						m.viewport.SetContent(m.renderMessages())
						m.viewport.GotoBottom()
						return m, tea.Batch(m.ollamaClient.startStreamCmd(ctx, m.modelName, m.messages, m.modelContextSize, m.stream, m.wg), waitForStreamCmd(m.stream), m.spinner.Tick)
					}
				}
			}
			// If it wasn't a tool call, the content is just plain text and is already accumulated correctly.

			// Final render
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
		case "/bye":
			return m, tea.Quit
		case "/help":
			m.messages = append(m.messages, Message{Role: "assistant", Content: "Commands:\n/bye - Exit the application /help - Show this help message /stop - Stop the current response /log - Toggle logging to a file /copy - Copy the last response to the clipboard"})
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
		role := "## " + strings.Title(msg.Role)

		var renderedMsg string
		if msg.IsError {
			md, _ := r.Render(fmt.Sprintf("%s\n\n%s\n\n---", role, msg.Content))
			content.WriteString(errorStyle.Render(md))
			continue
		} else {
			if msg.DisplayContent != "" {
				renderedMsg = msg.DisplayContent
			} else {
				renderedMsg = msg.Content
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

		md, _ := r.Render(fmt.Sprintf("%s\n\n%s\n\n---", role, renderedMsg))
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

func (m *model) View() string {
	if m.error != nil {
		return fmt.Sprintf("An error occurred: %v\n\nPress Ctrl+C to quit.", m.error)
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
			contextInfo = fmt.Sprintf("Context: %d", m.modelContextSize)
		} else {
			contextInfo = "Context: N/A"
		}

		footerText := fmt.Sprintf("Model: %s | %s | %s", m.modelName, contextInfo, stats)
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
