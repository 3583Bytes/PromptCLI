package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

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
type streamChunkMsg string
type streamDoneMsg struct{ stats string }
type errorMsg struct{ err error }

// --- Main Application Model ---

var footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray

type focusable int

const (
	focusTextarea focusable = iota
	focusViewport
)

type model struct {
	viewport         viewport.Model
	textarea         textarea.Model
	messages         []Message
	glamour          *glamour.TermRenderer
	apiURL           string
	modelName        string
	sending          bool
	error            error
	stats            string
	focused          focusable
	streaming        bool
	aiResponse       string
	stream           chan string
	cancel           context.CancelFunc
	fileSearchActive bool
	fileSearchTerm   string
	fileSearchResult string
	files            []string
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
	ta.BlurredStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")) // Gray

	vp.Style = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")). // Purple
		Padding(0) // Ensure no extra padding that could cause double border effect

	// --- File list ---
	files, err := os.ReadDir(".")
	if err != nil {
		log.Println("could not list files:", err)
	}
	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}

	return model{
		textarea:         ta,
		viewport:         vp,
		messages:         []Message{{Role: "system", Content: "You are a helpful assistant."}},
		apiURL:           apiURL,
		modelName:        modelName,
		sending:          false,
		stats:            "",
		focused:          focusTextarea,
		streaming:        false,
		aiResponse:       "",
		fileSearchActive: false,
		fileSearchTerm:   "",
		fileSearchResult: "",
		files:            fileNames,
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

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyTab:
			if m.fileSearchActive && m.fileSearchResult != "" {
				val := m.textarea.Value()
				re := regexp.MustCompile(`@\w*$`)
				newVal := re.ReplaceAllString(val, "@"+m.fileSearchResult)
				m.textarea.SetValue(newVal)
				m.fileSearchActive = false
				m.textarea.CursorEnd()
			}
		case tea.KeyEsc:
			if m.focused == focusTextarea {
				m.focused = focusViewport
				m.textarea.Blur()
			} else {
				m.focused = focusTextarea
				m.textarea.Focus()
			}
			return m, nil
		case tea.KeyCtrlC:
			return m, tea.Quit
		}

		if m.focused == focusTextarea {
			m.textarea, taCmd = m.textarea.Update(msg)

			// File search logic
			val := m.textarea.Value()
			re := regexp.MustCompile(`@(\w*)$`)
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
		} else {
			m.viewport, vpCmd = m.viewport.Update(msg)
		}

		// Handle Enter key for sending message
		if msg.Type == tea.KeyEnter && m.focused == focusTextarea {
			userInput := strings.TrimSpace(m.textarea.Value())
			if userInput == "/stop" {
				if m.cancel != nil {
					m.cancel()
				}
				m.streaming = false
				m.sending = false
				if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
					m.messages[len(m.messages)-1].Content += "\\n\\n--- Canceled ---"
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
					m.messages = append(m.messages, Message{Role: "assistant", Content: "Commands:\\n/bye - Exit the application\\n/help - Show this help message\\n/stop - Stop the current response"})
					m.viewport.SetContent(m.renderMessages())
					m.textarea.Reset()
					m.viewport.GotoBottom()
					return m, nil
				default:
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
					m.stream = make(chan string)
					m.messages = append(m.messages, Message{Role: "user", Content: userInput})
					m.messages = append(m.messages, Message{Role: "assistant", Content: ""})
					m.viewport.SetContent(m.renderMessages())
					m.textarea.Reset()
					m.viewport.GotoBottom()
					return m, tea.Batch(m.startStreamCmd(ctx), m.waitForStreamCmd())
				}
			}
		}

	case streamChunkMsg:
		if m.streaming {
			m.messages[len(m.messages)-1].Content += string(msg)
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()
			return m, m.waitForStreamCmd()
		}

	case streamDoneMsg:
		if m.streaming {
			m.streaming = false
			m.sending = false
			m.stats = msg.stats
		}
		return m, nil

	case errorMsg:
		m.sending = false
		m.error = msg.err

	case tea.WindowSizeMsg:
		newWidth := msg.Width - 2
		textAreaRenderedHeight := m.textarea.Height() + 2 + 1
		m.textarea.SetWidth(newWidth - 2)
		m.viewport.Width = newWidth
		m.viewport.Height = msg.Height - textAreaRenderedHeight
		m.viewport.SetContent(m.renderMessages())
		m.textarea, taCmd = m.textarea.Update(msg)
		m.viewport, vpCmd = m.viewport.Update(msg)

	default:
		m.textarea, taCmd = m.textarea.Update(msg)
		m.viewport, vpCmd = m.viewport.Update(msg)
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

	var footer string
	if m.fileSearchActive {
		footerText := "File search: "
		if m.fileSearchResult != "" {
			footerText += m.fileSearchResult
		} else {
			footerText += "No matches found"
		}
		footer = footerStyle.Render(footerText)
	} else {
		stats := "Response time and token stats will appear here."
		if m.stats != "" {
			stats = m.stats
		}
		footerText := fmt.Sprintf("Model: %s | %s", m.modelName, stats)
		footer = footerStyle.Render(footerText)
	}

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

// --- API Call (As a tea.Cmd) ---

func (m model) waitForStreamCmd() tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-m.stream
		if !ok {
			return streamDoneMsg{}
		}
		return streamChunkMsg(chunk)
	}
}

func (m model) startStreamCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		go func(ctx context.Context) {
			defer close(m.stream)

			req := ChatRequest{
				Model:    m.modelName,
				Messages: m.messages,
				Stream:   true,
			}
			reqBody, err := json.Marshal(req)
			if err != nil {
				return
			}

			httpReq, err := http.NewRequestWithContext(ctx, "POST", m.apiURL+"/api/chat", bytes.NewBuffer(reqBody))
			if err != nil {
				return
			}

			resp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			decoder := json.NewDecoder(resp.Body)
			for {
				var chatResp ChatResponse
				if err := decoder.Decode(&chatResp); err == io.EOF {
					break
				} else if err != nil {
					break
				}

				m.stream <- chatResp.Message.Content

				if chatResp.Done {
					break
				}
			}
		}(ctx)
		return nil
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