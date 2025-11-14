# Prompt CLI

A terminal-based code assistant & chat interface for interacting with [Ollama](https://ollama.ai) large language models (maybe others in future), simillar to Clude Code & Gemini CLI but designed to work with an Ollama API service (ollama serve) running on your network.  

Currently the focus is to bridge the communication between the LLM and the device, by sending a command to the LLM and receiving a command action that PromptCLI will process.  The smallest ollama model I had good results with is gpt-oss:20b.  Smaller models got lost within a few prompts or provided responses that did not match the JSON specifications in Prompt.MD.  gpt-oss:20b however conistantly provided me with good responses, and although not as smart as Cloude or ChatGTP it was functional.  llama3.1:8b was also ok, although more testing and implementation is still needed.

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
- **read_all_files**
  - purpose: read all files in a directory matching a glob pattern (e.g., "**/*.go"), concatenating their contents.
  - input: {"path": "string | nullable", "glob": "string", "max_bytes": "integer | null"}
  - notes: The output will be a single string where each file's content is preceded by a header like "--- File: path/to/file.go ---".
- **append_file** 
  - input: {"path": "string", "content": "string"}
- **delete_file** 
  - input: {"path": "string"}
- **respond** 
  - input: {"message": "string"}  // normal chat response for the user
- **git**
  - purpose: run read-only git queries compactly
  - input: {"cmd":"string|null","args":["string",... ]|null,"cwd":"string|null","timeout_ms":integer|null,"max_bytes":integer|null}
  - notes: Read-only only; ask for confirmation before any mutating action. The 'args' field must be an array of strings, with each command line argument as a separate string in the array. See Example E.
- **web_search**
  - purpose: perform a simple web search and return a short summary and top links
  - input: {"q":"string","max_results":"integer | null","site":"string | null","recency_days":"integer | null"}
  - notes: Use when you need fresh or external info. Keep queries concise. Results will be returned by the host as:
           {"summary":"string","results":[{"title":"string","url":"string","snippet":"string"}...]}
- **visit_url**
  - purpose: fetch and summarize the content of a specific web page
  - input: {"url":"string","max_bytes":"integer | null"}
  - notes: Use only for URLs from trusted sources (e.g., from web_search results).
           The host will return a concise summary and key text from the page.

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
  - `/new` – New session freeing up context window
  - `/log` – Toggle logging
  
  - `@` - Reference a file in the current or sub folder to upload as part of the chat context.

---

## 📦 Currently Out of Scope

No Agentic Loops that go through a task list.  Also no cashing or diffs or token saving methods, since the model is a local LLM we don't pay per token, we can focus on accuracy rather than saving tokens.

## 📦 Installation

Clone this repo and build:

```bash
git clone https://github.com/3583Bytes/PromptCLI.git
cd PromptCLI
go build -o .build/promptcli.exe
