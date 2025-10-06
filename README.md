# Prompt CLI

A terminal-based code assistant & chat interface for interacting with [Ollama](https://ollama.ai) large language models (maybe others in future), simillar to Clude Code & Gemini CLI but designed to work with an Ollama service running on your network.  Currently the focus is to bridge the communication between the LLM and the device rather than creating a fully automatic agent.  For example I want the LLM to be able to look at git commits & diffs, be able to read files, write files etc.

---

## ✨ How it works

The LLM is forced to use json as it's communication method via Prompt.MD and then the json can be parsed into commands that are executed by Prompt CLI.

## ✨ Features
- **Interactive TUI** for chatting with Ollama models.
- **Streaming responses** with cancel support (`/stop`).
- **Configurable default model** via `config.json`.
- **Configurable initial Prompt** via `Prompt.MD`.
- **Automatic model discovery** from your Ollama server.
- **Inline file injection**: reference local files using `@filename` and their contents will be inserted into the conversation.
- **Basic commands**:
  - `/help` – Show available commands  
  - `/bye` – Exit the application  
  - `/stop` – Stop the current response mid-stream  
  - `@` - Reference a file in the current or sub folder to upload as part of the chat context.

---

## 📦 Installation

Clone this repo and build:

```bash
git clone https://github.com/3583Bytes/PromptCLI.git
cd PromptCLI
go build -o .build/prompt-cli.exe main.go
