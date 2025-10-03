package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// --- Config ---
type Config struct {
	OllamaServerURL  string `json:"ollama_server_url"`
	OllamaServerPort int    `json:"ollama_server_port"`
	DefaultLLM       string `json:"default_llm"`
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	config := &Config{}
	err = decoder.Decode(config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// --- API Data Structures ---
type TagsResponse struct {
	Models []Model `json:"models"`
}
type Model struct {
	Name string `json:"name"`
}
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ChatResponse struct {
	Message   Message   `json:"message"`
	Done      bool      `json:"done"`
	EvalCount int       `json:"eval_count"`
}

// --- Bubble Tea Messages ---
type responseMsg struct {
	content string
	stats   string
}
type errorMsg struct{ err error }

// --- Main Application Model ---

type model struct {
	viewport    viewport.Model
	textarea    textarea.Model
	messages    []Message
	glamour     *glamour.TermRenderer
	apiURL      string
	modelName   string
	sending     bool
	error       error
}

func initialModel(apiURL, modelName string) model {
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

	// --- Styles ---
	ta.FocusedStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")) // Orange

	vp.Style = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")). // Purple
		Padding(0) // Ensure no extra padding that could cause double border effect

	return model{
		textarea:    ta,
		viewport:    vp,
		messages:    []Message{{Role: "system", Content: "You are a helpful assistant."}},
		apiURL:      apiURL,
		modelName:   modelName,
		sending:     false,
	}
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		taCmd tea.Cmd
		vpCmd tea.Cmd
	)

	m.textarea, taCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if !m.sending {
				userInput := strings.TrimSpace(m.textarea.Value())
				switch userInput {
				case "/bye":
					return m, tea.Quit
				case "/help":
					m.messages = append(m.messages, Message{Role: "assistant", Content: "Commands:\n/bye - Exit the application\n/help - Show this help message"})
					m.viewport.SetContent(m.renderMessages())
					m.textarea.Reset()
					m.viewport.GotoBottom()
					return m, nil
				default:
					m.sending = true
					m.messages = append(m.messages, Message{Role: "user", Content: userInput})
					m.viewport.SetContent(m.renderMessages())
					m.textarea.Reset()
					m.viewport.GotoBottom()
					return m, m.streamResponse()
				}
			}
		}

	// --- Custom Messages ---
	case responseMsg:
		m.sending = false
		// Append AI response content
		m.messages = append(m.messages, Message{Role: "assistant", Content: msg.content})
		content := m.renderMessages()

		// Append stats to the conversation view
		statsContent, _ := glamour.NewTermRenderer(glamour.WithAutoStyle())
		renderedStats, _ := statsContent.Render(fmt.Sprintf("\n---\n*%s*", msg.stats))
		m.viewport.SetContent(content + renderedStats)
		m.viewport.GotoBottom()
		return m, nil

	case errorMsg:
		m.sending = false
		m.error = msg.err
		return m, nil

	case tea.WindowSizeMsg:
		// 2 characters for side borders (left and right)
		newWidth := msg.Width - 2

		// textarea content height is 3, plus 2 for top/bottom borders
		// plus 1 for the newline separator between viewport and textarea
		textAreaRenderedHeight := m.textarea.Height() + 2

		// an additional 3 characters to compensate for an unknown rendering difference.
		m.textarea.SetWidth(newWidth - 2)
		m.viewport.Width = newWidth
		m.viewport.Height = msg.Height - textAreaRenderedHeight
		m.viewport.SetContent(m.renderMessages())
		return m, nil
	}

	return m, tea.Batch(taCmd, vpCmd)
}

func (m *model) renderMessages() string {
	// Re-create renderer with the correct width, accounting for viewport padding
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(m.viewport.Width-2),
	)

	var content strings.Builder
	for _, msg := range m.messages {
		if msg.Role == "system" {
			continue
		}
		role := "## " + strings.Title(msg.Role)
		md, _ := r.Render(fmt.Sprintf("%s\n\n%s\n\n---", role, msg.Content))
		content.WriteString(md)
	}
	return content.String()
}

func (m model) View() string {
	if m.error != nil {
		return fmt.Sprintf("An error occurred: %v\n\nPress Ctrl+C to quit.", m.error)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewport.View(),
		m.textarea.View(),
	)
}

// --- API Call (As a tea.Cmd) ---

func (m model) streamResponse() tea.Cmd {
	return func() tea.Msg {
		req := ChatRequest{
			Model:    m.modelName,
			Messages: m.messages,
			Stream:   true,
		}
		reqBody, err := json.Marshal(req)
		if err != nil {
			return errorMsg{err}
		}

		resp, err := http.Post(m.apiURL+"/api/chat", "application/json", bytes.NewBuffer(reqBody))
		if err != nil {
			return errorMsg{err}
		}
		defer resp.Body.Close()

		startTime := time.Now()
		var fullResponse string
		var finalResponse ChatResponse

		decoder := json.NewDecoder(resp.Body)
		for {
			var chatResp ChatResponse
			if err := decoder.Decode(&chatResp); err == io.EOF {
				break
			} else if err != nil {
				return errorMsg{err}
			}

			fullResponse += chatResp.Message.Content

			if chatResp.Done {
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

		return responseMsg{content: fullResponse, stats: stats}
	}
}

// --- Pre-BubbleTea Setup ---

func main() {
	// Load configuration
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	baseURL := fmt.Sprintf("%s:%d", config.OllamaServerURL, config.OllamaServerPort)

	models, err := getModels(baseURL)
	if err != nil {
		log.Fatalf("Error getting models: %v", err)
	}
	if len(models) == 0 {
		log.Fatal("No models found on the Ollama server.")
	}

	var selectedModel string
	if config.DefaultLLM != "" {
		selectedModel = config.DefaultLLM
	} else {
		fmt.Println("Please select a model:")
		for i, m := range models {
			fmt.Printf("%d: %s\n", i+1, m.Name)
		}

		var choice int
		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("> ")
			input, _ := reader.ReadString('\n')
			choice, err = strconv.Atoi(strings.TrimSpace(input))
			if err == nil && choice > 0 && choice <= len(models) {
				break
			}
			fmt.Println("Invalid choice, please try again.")
		}
		selectedModel = models[choice-1].Name
	}

	// Start the Bubble Tea program
	p := tea.NewProgram(initialModel(baseURL, selectedModel), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		log.Fatalf("Alas, there's been an error: %v", err)
	}
}

func getModels(baseURL string) ([]Model, error) {
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tagsResponse TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		return nil, err
	}
	return tagsResponse.Models, nil
}