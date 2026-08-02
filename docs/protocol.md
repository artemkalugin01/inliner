# Inliner Editor Protocol

This document describes the current protocol between `inliner.nvim` and
`inliner-core`. It is a contract for the two projects in this monorepo; it is not
loaded by the application at runtime.

## Transport

`inliner.nvim` starts `inliner-core stdio` as a long-running stdio process.

Other core commands:

- `inliner-core version`: prints the core version and exits.
- `inliner-core test-ollama`: tests the configured Ollama endpoint/model, prints a short response preview, and exits.

Editor to core:

- One JSON object per line on stdin.
- Messages are internally tagged with a `kind` field.

Core to editor:

- One JSON object per line on stdout.
- Protocol lines are prefixed with `INLINER-MESSAGE `.
- Non-prefixed output is not part of the protocol and must be ignored by the editor.

Example core output:

```text
INLINER-MESSAGE {"kind":"response","stateId":"1","items":[{"kind":"text","text":" // inliner"},{"kind":"end"}]}
```

## Core Configuration

`inliner-core` is configured with environment variables.

Provider selection:

- `INLINER_PROVIDER`: `fake` or `ollama`, default `fake`.

Ollama settings:

- `INLINER_OLLAMA_BASE_URL`: default `http://127.0.0.1:11434`.
- `INLINER_OLLAMA_MODEL`: default `qwen2.5-coder:7b`.
- `INLINER_OLLAMA_TEMPERATURE`: default `0.1`, valid range `0..2`.
- `INLINER_OLLAMA_NUM_PREDICT`: default `128`, must be greater than zero.

Core request settings:

- `INLINER_REQUEST_TIMEOUT`: default `3s`, parsed as a Go duration.
- `INLINER_WINDOW_BYTES`: default `2000`, byte window on each side of the cursor.

Prompt context budget settings:

- `INLINER_PROMPT_MAX_FILES`: default `20`.
- `INLINER_PROMPT_MAX_IMPORTS`: default `80`.
- `INLINER_PROMPT_MAX_TYPES`: default `80`.
- `INLINER_PROMPT_MAX_INTERFACES`: default `40`.
- `INLINER_PROMPT_MAX_INTERFACE_METHODS`: default `12`.
- `INLINER_PROMPT_MAX_VISIBLE`: default `80`.
- `INLINER_PROMPT_MAX_FUNCTIONS`: default `120`.

Debug logging settings:

- `INLINER_DEBUG_VERBOSE`: default `false`. Set to `1` or `true` to enable verbose debug logging.
- `INLINER_DEBUG_DIR`: default system temp directory plus `inliner-debug`.

When verbose debug logging is enabled, Ollama requests write:

- `completion-timings.log`: one line per model request with timestamp, model, state id, file, duration, status, and error.
- `prompts/*.prompt.txt`: one prompt per file, named with a UTC timestamp. The first line is `projectHash`, a SHA-256 hash of the absolute project path for log correlation only. The second line contains Ollama model metadata. These metadata lines are not included in the prompt sent to the model.

Debug prompts contain source context and should be treated as sensitive project data.

## Editor To Core

### `greeting`

Sent after the core process starts.

```json
{"kind":"greeting","allowGitignore":false}
```

Fields:

- `allowGitignore`: boolean. Reserved for future repository-context behavior.

Current core behavior:

- Emits `connection_status` with `is_connected:true`.

### `state_update`

Synchronizes buffer state and/or requests completion at a cursor position.

```json
{
  "kind": "state_update",
  "newId": "42",
  "updates": [
    {"kind":"file_update","path":"/tmp/main.go","content":"package main\n"},
    {"kind":"cursor_update","path":"/tmp/main.go","offset":12}
  ]
}
```

Fields:

- `newId`: editor-generated request/state identifier.
- `updates`: ordered list of state updates.

Supported update kinds:

- `file_update`: full current buffer content for `path`.
- `cursor_update`: completion request for `path` at byte `offset`.

Current core behavior:

- Stores `file_update` content in memory.
- Uses `cursor_update` to create an async completion request.
- For Go files that exist on disk, may enrich the request with current-package declarations before calling the provider.
- The current buffer's unsaved content is overlaid into package parsing for the active file.
- Package context is budgeted before being added to the model prompt.
- Echoes `newId` back as `stateId` in a `response`.
- Cancels older in-flight completion requests for the same file.
- Suppresses stale responses if a newer state for the same file exists.

### `shutdown`

Requests clean process shutdown.

```json
{"kind":"shutdown"}
```

Current core behavior:

- Cancels in-flight completion requests.
- Waits for completion workers to exit.
- Returns from the stdio loop.

### `health_request`

Requests current core health/configuration information.

```json
{"kind":"health_request"}
```

Current core behavior:

- Emits `health_response`.
- Runs provider diagnostics. Ollama diagnostics perform a lightweight `GET /api/tags` using the configured request timeout.

### `accept_update`

Sent after the editor accepts a completion suggestion.

```json
{
  "kind": "accept_update",
  "stateId": "42",
  "path": "/tmp/main.go",
  "text": "fmt.Println(name)"
}
```

Fields:

- `stateId`: the accepted completion's response state id.
- `path`: file path where the suggestion was accepted.
- `text`: exact text inserted by the editor.

Current core behavior:

- Validates `stateId` and `path`.
- Does not emit a response.
- Stores the accepted text in an in-memory acceptance cache when matching request context is still known.
- The cache is local to the running core process and is not persisted.

Future uses:

- prompt quality heuristics
- accepted/rejected suggestion tracking without external telemetry

### `dismiss_update`

Sent after the editor explicitly dismisses a visible completion suggestion.

Automatic UI clears caused by cursor movement, text changes, insert leave, or
buffer leave do not send this message.

```json
{
  "kind": "dismiss_update",
  "stateId": "42",
  "path": "/tmp/main.go",
  "text": "fmt.Println(name)"
}
```

Fields:

- `stateId`: the dismissed completion's response state id.
- `path`: file path where the suggestion was dismissed.
- `text`: exact suggestion text dismissed by the editor.

Current core behavior:

- Validates `stateId` and `path`.
- Does not emit a response.
- Stores the dismissed text in an in-memory dismissal cache when matching request context is still known.
- Suppresses future matching completions for the same nearby context by returning only `end`.
- The cache is local to the running core process and is not persisted.

## Core To Editor

### `connection_status`

Reports whether the core is ready to receive requests.

```json
{"kind":"connection_status","is_connected":true,"status_text":null}
```

Fields:

- `is_connected`: boolean.
- `status_text`: nullable string.

### `response`

Returns completion items for a previous `state_update`.

```json
{
  "kind": "response",
  "stateId": "42",
  "items": [
    {"kind":"text","text":"fmt.Println(name)"},
    {"kind":"end"}
  ]
}
```

Fields:

- `stateId`: the original `newId` from the editor.
- `items`: ordered completion response items.

Supported item kinds:

- `text`: text to show as ghost text and insert if accepted.
- `end`: terminates the completion stream.

Reserved item kinds:

- `barrier`: future stale/overrun boundary marker.
- `delete`: future replacement support.

Current editor behavior:

- Ignores responses whose `stateId` is not latest for that buffer.
- Concatenates `text` items until `end` or `barrier`.
- Renders the first completion line as inline ghost text.
- Renders remaining completion lines as virtual lines below.

Current core behavior:

- Provider output may be post-processed before response emission.
- Ollama output is cleaned of simple markdown/code fences.
- Ollama output is trimmed when it duplicates text already present after the cursor.

### `error`

Reports recoverable protocol or completion errors.

```json
{"kind":"error","message":"no document content for \"/tmp/main.go\""}
```

Fields:

- `message`: human-readable error string.

Current editor behavior:

- Shows the message with `vim.notify` at warning level.

### `health_response`

Returns current core provider/config/session state.

```json
{
  "kind": "health_response",
  "coreVersion": "0.1.0",
  "provider": "ollama",
  "ollamaBaseUrl": "http://127.0.0.1:11434",
  "ollamaModel": "qwen2.5-coder:7b",
  "ollamaTemperature": 0.1,
  "ollamaNumPredict": 128,
  "providerStatus": "ok",
  "providerReachable": true,
  "requestTimeout": "3s",
  "windowBytes": 2000,
  "openDocuments": 1,
  "inFlightRequests": 0
}
```

Fields:

- `provider`: active provider name.
- `coreVersion`: active `inliner-core` version.
- `ollamaBaseUrl`: configured Ollama base URL when present.
- `ollamaModel`: configured Ollama model when present.
- `ollamaTemperature`: configured Ollama temperature.
- `ollamaNumPredict`: configured Ollama token prediction limit.
- `providerStatus`: short diagnostic status, such as `ok`, `unhealthy`, or `unreachable`.
- `providerReachable`: whether the provider's diagnostic check succeeded.
- `providerError`: optional provider diagnostic error string.
- `requestTimeout`: configured request timeout as a duration string.
- `windowBytes`: configured cursor context window size.
- `openDocuments`: number of documents currently tracked by core.
- `inFlightRequests`: number of active provider requests.

Current editor behavior:

- `:InlinerStart` sends a startup `health_request` after `greeting` and displays a concise connected message.
- `setup({ auto_start = true })` starts the core automatically when entering a supported buffer.
- `:InlinerComplete` manually sends a completion request for the current cursor position.
- `setup({ complete_key = "<key>" })` optionally maps a manual completion key.
- `:InlinerAcceptWord` accepts the next word-sized chunk from the visible suggestion.
- `setup({ accept_word_key = "<key>" })` optionally maps partial word acceptance.
- `:InlinerEnable`, `:InlinerDisable`, and `:InlinerToggle` control automatic suggestions.
- `setup({ auto_suggest = false })` starts with automatic suggestions disabled.
- `setup({ minimum_core_version = "0.1.0" })` controls the minimum compatible core version.
- `:InlinerHealth` sends `health_request` when the core is running.
- Displays `health_response` with `vim.notify`.
- Reports locally if the core is not running.
- Warns if `health_response.coreVersion` is older than `minimum_core_version`.

## Offsets

All offsets are byte offsets into the latest `file_update` content for the same
path.

Current Neovim calculation:

```lua
vim.api.nvim_buf_get_offset(bufnr, row) + col
```

The core clamps offsets outside the current document bounds before extracting
completion context.

## Ordering And Staleness

Both sides defend against stale completions.

Core behavior:

- A new `cursor_update` for a file cancels the previous completion for that file.
- A new `file_update` for a file cancels the previous completion for that file.
- If a cancelled provider still returns, core suppresses its response.

Editor behavior:

- Tracks the latest `stateId` per buffer.
- Ignores any `response` that does not match the latest state for that buffer.

## Current Limitations

- Go-only completion pipeline.
- Text changes currently send full buffer content.
- Package context overlays the current unsaved buffer, but unsaved declarations in other files are not reflected yet.
- Prompt context budgets are count-based, not token-based.
- No `gopls` context yet.
- No MCP context yet.
- Accepted suggestions are reported with `accept_update`.
- Explicitly dismissed suggestions are reported with `dismiss_update`.

## Change Policy

Any protocol change should update these files together:

- `docs/protocol.md`
- `inliner-core/internal/protocol/messages.go`
- relevant `inliner-core` protocol/session tests
- relevant `inliner.nvim` send/receive handling

Current verification commands:

- `go test ./...` from `inliner-core/`
- `nvim --headless -u NONE -l tests/run.lua` from `inliner.nvim/`
