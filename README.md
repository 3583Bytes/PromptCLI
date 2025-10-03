# Prompt CLI

A terminal-based chat interface for interacting with [Ollama](https://ollama.ai) large language models.  
Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and [Glamour](https://github.com/charmbracelet/glamour).

---

## ✨ Features
- **Interactive TUI** for chatting with Ollama models.
- **Streaming responses** with cancel support (`/stop`).
- **Configurable default model** via `config.json`.
- **Automatic model discovery** from your Ollama server.
- **Inline file injection**: reference local files using `@filename` and their contents will be inserted into the conversation.
- **Basic commands**:
  - `/help` – Show available commands  
  - `/bye` – Exit the application  
  - `/stop` – Stop the current response mid-stream  

---

## 📦 Installation

Clone this repo and build:

```bash
git clone https://github.com/3583Bytes/Prompt-CLI.git
cd Prompt-CLI
go build -o prompt-cli.exe main.go
