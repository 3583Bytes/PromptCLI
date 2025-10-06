# Prompt CLI

A terminal-based code assistant & chat interface for interacting with [Ollama](https://ollama.ai) large language models (maybe others in future), simillar to Clude Code & Gemini CLI but designed to work with an Ollama service running on your network.  Currently the focus is to bridge the communication between the LLM and the device rather than creating a fully automatic agent.  This means the workflow is focused on sending a command to the LLM and receiving a command action, rather than creating a loop of commands until a set of tasks is done.  This may change in the future as local LLMs get more powerfull.

---

## ✨ How it works

The LLM is forced to use json as it's communication method via Prompt.MD and then the json can be parsed into commands that are executed by Prompt CLI.  Currently supported commands are:

- **list_files** 
  - purpose: list files in a directory (non-recursive)
  - input: {"path": "string | null", "glob": "string | null"}
- **read_file** 
  - input: {"path": "string", "max_bytes": "integer | null"}
- **write_file** 
  - input: {"path": "string", "content": "string", "mode": "overwrite | create_only"}
- **append_file** 
  - input: {"path": "string", "content": "string"}
- **delete_file** 
  - input: {"path": "string"}
- **respond** 
  - input: {"message": "string"}  // normal chat response for the user

## ✨ Current Features
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

## 📦 Currently Out of Scope

No Agentic Loops that go through a task list.  Also no cashing or diffs or token saving methods, since the model is a local LLM we don't pay per token, we can focus on accuracy rather than saving tokens.

## 📦 Installation

Clone this repo and build:

```bash
git clone https://github.com/3583Bytes/PromptCLI.git
cd PromptCLI
go build -o .build/prompt-cli.exe main.go
