# Inliner VS Code

Thin VS Code client for `inliner-core`.

The extension starts `inliner-core stdio`, sends full Go document content plus cursor byte offset, and renders responses through VS Code inline completions.

## Behavior

- Supports Go files.
- Uses VS Code inline completions for ghost text.
- Sends `accept_update` to core when VS Code accepts an inline suggestion.
- Does not currently receive an automatic dismiss event from VS Code.

## Settings

- `inliner.coreCommand`: explicit command array for starting core, for example `["/path/to/inliner-core", "stdio"]`.
- `inliner.autoStart`: start core automatically for Go files.
- `inliner.provider`: `fake` or `ollama`.
- `inliner.ollamaModel`: defaults to `qwen2.5-coder:7b`.
- `inliner.debugVerbose`: enables core prompt/timing logs. Prompt logs contain source context.
- `inliner.debugDir`: optional debug log directory.
- `inliner.requestTimeoutMs`: VS Code-side response timeout.

## Development

```sh
npm install
npm run compile
```

For monorepo development, build core first if you do not have `inliner-core` on `PATH`:

```sh
make -C ../inliner-core build
```

Useful commands:

- `Inliner: Start`
- `Inliner: Stop`
- `Inliner: Health`
- `Inliner: Complete`

By default the extension uses Ollama with `qwen2.5-coder:7b`.
