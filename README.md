# Inliner

Inliner is a Go-focused inline autocomplete project. It provides ghost-text completions for editors while keeping editor integrations thin and moving protocol, context collection, prompt building, model providers, diagnostics, telemetry, and edit-history intelligence into `inliner-core`.

The current primary provider is local Ollama. The core is specialized for Go context, but provider and context internals are structured so they can be replaced or extended later.

## Project Structure

- `inliner-core/`: Go core process. It speaks newline-delimited JSON over stdio, collects Go context, builds prompts, calls providers, tracks recent edits, and emits diagnostics/telemetry.
- `inliner.nvim/`: Neovim plugin. Starts core, sends buffer/cursor updates, renders ghost text, accepts/dismisses suggestions, and exposes UX/debug/model commands.
- `inliner.vscode/`: VS Code extension. Starts core and exposes Go inline completions through the VS Code inline completion API.
- `docs/`: Protocol and roadmap documentation.

## Core Commands

Run from `inliner-core/`:

```sh
go run ./cmd/inliner-core version
go run ./cmd/inliner-core stdio
go run ./cmd/inliner-core test-ollama
```

Build and test:

```sh
make build
make test
```

Or directly:

```sh
go test ./...
go build ./cmd/inliner-core
```

## Starting With Ollama

Install and start Ollama, then pull a coding model:

```sh
ollama serve
ollama pull qwen2.5-coder:7b
```

Run the core with Ollama:

```sh
INLINER_PROVIDER=ollama \
INLINER_OLLAMA_MODEL=qwen2.5-coder:7b \
go run ./cmd/inliner-core stdio
```

Check provider connectivity:

```sh
INLINER_PROVIDER=ollama \
INLINER_OLLAMA_MODEL=qwen2.5-coder:7b \
go run ./cmd/inliner-core test-ollama
```

## Neovim

Minimal setup:

```lua
require("inliner").setup({
  auto_start = true,
  auto_suggest = true,
  accept_key = "<Tab>",
  accept_word_key = "\\1",
  ollama_model = "qwen2.5-coder:7b",
})
```

Useful commands:

- `:InlinerStart`: start core.
- `:InlinerStop`: stop core.
- `:InlinerComplete`: request a completion manually.
- `:InlinerAccept`: accept current suggestion.
- `:InlinerAcceptWord`: accept next word from current suggestion.
- `:InlinerDismiss`: dismiss current suggestion.
- `:InlinerHealth`: show core/provider health.
- `:InlinerEnable`, `:InlinerDisable`, `:InlinerToggle`: control automatic suggestions.
- `:InlinerOpenDebugDir`: open debug directory.
- `:InlinerOpenTimingLog`: open `completion-timings.log`.
- `:InlinerOpenTelemetryLog`: open `request-lifecycle.jsonl`.
- `:InlinerOpenLatestPrompt`: open latest prompt log.
- `:InlinerToggleDebug`: toggle debug logging for the next core start.
- `:InlinerStatus`: print status/debug state.
- `:InlinerListModels`: list Ollama models.
- `:InlinerPickModel`: select an Ollama model with `vim.ui.select`.
- `:InlinerSwitchModel <model>`: switch Ollama model and restart core if running.
- `:InlinerTestCompletion`: request a test completion through the normal manual path.
- `:InlinerModelInfo`: show model/provider/resource information.

Neovim config fields:

```lua
require("inliner").setup({
  cmd = nil,
  cwd = nil,
  allow_gitignore = false,
  accept_key = "<Tab>",
  accept_word_key = nil,
  auto_suggest = true,
  auto_start = false,
  complete_key = nil,
  debug_dir = vim.fs.joinpath(vim.fn.stdpath("cache"), "inliner-debug"),
  debug_verbose = false,
  dismiss_key = "<C-]>",
  debounce_ms = 120,
  filetypes = { go = true },
  minimum_core_version = "0.1.0",
  ollama_base_url = "http://127.0.0.1:11434",
  ollama_model = nil,
  suppress_expected_provider_errors = true,
  suppress_in_comments_strings = false,
})
```

## VS Code

Run from `inliner.vscode/`:

```sh
npm install
npm run compile
```

The extension starts `inliner-core stdio`, sends Go document/cursor updates, and provides inline completions. Commands include start, stop, health, and manual completion.

## Environment Variables

Provider settings:

- `INLINER_PROVIDER`: `fake` or `ollama`. Default: `fake`.
- `INLINER_OLLAMA_BASE_URL`: Ollama base URL. Default: `http://127.0.0.1:11434`.
- `INLINER_OLLAMA_MODEL`: Ollama model. Default: `qwen2.5-coder:7b`.
- `INLINER_OLLAMA_TEMPERATURE`: generation temperature, valid `0..2`. Default: `0.1`.
- `INLINER_OLLAMA_NUM_PREDICT`: max generated tokens. Default: `128`.

Request settings:

- `INLINER_REQUEST_TIMEOUT`: request/provider timeout as Go duration. Default: `3s`.
- `INLINER_WINDOW_BYTES`: bytes of prefix/suffix around cursor. Default: `2000`.

Prompt context budgets:

- `INLINER_PROMPT_MAX_FILES`: default `20`.
- `INLINER_PROMPT_MAX_IMPORTS`: default `80`.
- `INLINER_PROMPT_MAX_TYPES`: default `80`.
- `INLINER_PROMPT_MAX_INTERFACES`: default `40`.
- `INLINER_PROMPT_MAX_INTERFACE_METHODS`: default `12`.
- `INLINER_PROMPT_MAX_VISIBLE`: default `80`.
- `INLINER_PROMPT_MAX_SIBLINGS`: default `40`.
- `INLINER_PROMPT_MAX_VALUES`: default `80`.
- `INLINER_PROMPT_MAX_FUNCTIONS`: default `120`.

Debug and telemetry:

- `INLINER_DEBUG_VERBOSE`: enables verbose prompt/timing logs. Default: `false`.
- `INLINER_DEBUG_DIR`: debug output directory. Default: system temp directory plus `inliner-debug` for core; Neovim passes `stdpath("cache")/inliner-debug` by default.
- `INLINER_TELEMETRY_ENABLED`: writes core-only request lifecycle telemetry as JSONL. Default: `false`.

When debug logging is enabled, core writes:

- `prompts/*.prompt.txt`: prompt logs with metadata header.
- `completion-timings.log`: one line per Ollama request.

When telemetry is enabled, core writes:

- `request-lifecycle.jsonl`: one JSON object per completion request with lifecycle timings and minimal metadata. It avoids source/prompt text and uses file/project hashes.

## Examples

Run core with Ollama and telemetry:

```sh
INLINER_PROVIDER=ollama \
INLINER_OLLAMA_MODEL=qwen2.5-coder:7b \
INLINER_DEBUG_VERBOSE=true \
INLINER_TELEMETRY_ENABLED=true \
INLINER_DEBUG_DIR=/tmp/inliner-debug \
go run ./cmd/inliner-core stdio
```

Use a larger context window and longer provider timeout:

```sh
INLINER_PROVIDER=ollama \
INLINER_WINDOW_BYTES=4000 \
INLINER_REQUEST_TIMEOUT=8s \
go run ./cmd/inliner-core stdio
```

Switch model from Neovim:

```vim
:InlinerListModels
:InlinerSwitchModel qwen2.5-coder:7b
:InlinerModelInfo
```

### Live Core Diagnostics

Start the standalone diagnostic process before starting or restarting `inliner-core`:

```sh
inliner-core debug
```

It follows completion requests live and reports context preparation time, model wait time, and the final result. To include each complete model prompt:

```sh
inliner-core debug --verbose
```

The diagnostic process communicates with core over a local Unix socket. If it is not running when core starts, diagnostics use a no-op publisher with no socket writes or background diagnostic goroutine.

## Development Verification

Run all current checks:

```sh
(cd inliner-core && go test ./...)
(cd inliner.nvim && nvim --headless -u NONE -l tests/run.lua)
(cd inliner.vscode && npm run compile)
```
