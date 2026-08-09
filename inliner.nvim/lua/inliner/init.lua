local client = require("inliner.client")
local ghost_text = require("inliner.ghost_text")
local version = require("inliner.version")

local M = {}

local defaults = {
  cmd = nil,
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
}

local state = {
  config = vim.deepcopy(defaults),
  client = nil,
  augroup = nil,
  seq = 0,
  latest_state_id_by_buf = {},
  buf_by_state_id = {},
  cursor_by_state_id = {},
  pending_by_buf = {},
  suggestion_by_buf = {},
  pending_health_kind = nil,
  last_core_error = nil,
  last_health = nil,
  selected_model = defaults.ollama_model,
  cached_models = {},
  auto_suggest_enabled = defaults.auto_suggest,
  mapped_keys = {},
}

local uv = vim.uv or vim.loop

local function clear_suggestion(bufnr)
  state.suggestion_by_buf[bufnr] = nil
  ghost_text.clear(bufnr)
end

local function default_cmd()
  local binary = vim.fn.exepath("inliner-core")
  if binary ~= "" then
    return { binary, "stdio" }
  end

  local source = debug.getinfo(1, "S").source:sub(2)
  local plugin_root = vim.fs.dirname(vim.fs.dirname(vim.fs.dirname(source)))
  local core_root = vim.fs.joinpath(vim.fs.dirname(plugin_root), "inliner-core")

  return { "go", "run", "./cmd/inliner-core", "stdio" }, core_root
end

local function is_supported(bufnr)
  local ft = vim.bo[bufnr].filetype
  return state.config.filetypes[ft] == true
end

local function file_path(bufnr)
  local name = vim.api.nvim_buf_get_name(bufnr)
  if name == "" then
    return nil
  end
  return name
end

local function buffer_content(bufnr)
  return table.concat(vim.api.nvim_buf_get_lines(bufnr, 0, -1, true), "\n") .. "\n"
end

local function cursor_position()
  local cursor = vim.api.nvim_win_get_cursor(0)
  return cursor[1] - 1, cursor[2]
end

local function cursor_offset(bufnr, row, col)
  return vim.api.nvim_buf_get_offset(bufnr, row) + col
end

local function debug_dir()
  return state.config.debug_dir or defaults.debug_dir
end

local function ollama_base_url()
  return vim.trim(state.config.ollama_base_url or defaults.ollama_base_url):gsub("/+$", "")
end

local function selected_model()
  local model = state.selected_model or state.config.ollama_model
  if model == nil or model == "" then
    return nil
  end
  return model
end

local function core_env()
  local env = {
    INLINER_DEBUG_DIR = debug_dir(),
    INLINER_DEBUG_VERBOSE = state.config.debug_verbose and "true" or "false",
    INLINER_OLLAMA_BASE_URL = ollama_base_url(),
  }
  local model = selected_model()
  if model then
    env.INLINER_OLLAMA_MODEL = model
  end
  return env
end

local function parse_ollama_models(payload)
  local ok, decoded = pcall(vim.json.decode, payload or "")
  if not ok or type(decoded) ~= "table" or type(decoded.models) ~= "table" then
    return nil, "failed to decode Ollama model list"
  end
  local models = {}
  for _, model in ipairs(decoded.models) do
    if type(model) == "table" and type(model.name) == "string" and model.name ~= "" then
      models[#models + 1] = {
        name = model.name,
        size = model.size,
        modified_at = model.modified_at,
        digest = model.digest,
      }
    end
  end
  table.sort(models, function(a, b)
    return a.name < b.name
  end)
  return models, nil
end

local function format_bytes(value)
  value = tonumber(value)
  if not value then
    return "unknown"
  end
  local units = { "B", "KiB", "MiB", "GiB", "TiB" }
  local index = 1
  while value >= 1024 and index < #units do
    value = value / 1024
    index = index + 1
  end
  if index == 1 then
    return string.format("%d %s", value, units[index])
  end
  return string.format("%.1f %s", value, units[index])
end

local function is_expected_provider_error(message)
  message = tostring(message or ""):lower()
  return message:find("context deadline exceeded", 1, true) ~= nil
    or message:find("connection refused", 1, true) ~= nil
    or message:find("connection reset", 1, true) ~= nil
    or message:find("no route to host", 1, true) ~= nil
    or message:find("ollama", 1, true) ~= nil and message:find("failed", 1, true) ~= nil
end

local function cursor_is_comment_or_string(bufnr)
  if not state.config.suppress_in_comments_strings then
    return false
  end
  if not vim.api.nvim_buf_is_valid(bufnr) then
    return false
  end

  local row, col = cursor_position()
  local syn_id = vim.fn.synID(row + 1, col + 1, 1)
  local translated = vim.fn.synIDtrans(syn_id)
  local name = (vim.fn.synIDattr(translated, "name") or ""):lower()
  return name:find("comment", 1, true) ~= nil or name:find("string", 1, true) ~= nil
end

local function open_path(path)
  if not path or path == "" then
    vim.notify("inliner path is not available", vim.log.levels.WARN)
    return false
  end
  vim.cmd.edit(vim.fn.fnameescape(path))
  return true
end

local function latest_file(pattern)
  local files = vim.fn.glob(pattern, false, true)
  local latest = nil
  local latest_mtime = -1
  for _, path in ipairs(files) do
    local stat = uv.fs_stat(path)
    if stat and stat.mtime and stat.mtime.sec > latest_mtime then
      latest = path
      latest_mtime = stat.mtime.sec
    end
  end
  return latest
end

local function next_state_id(bufnr, row, col)
  state.seq = state.seq + 1
  local id = tostring(state.seq)
  state.latest_state_id_by_buf[bufnr] = id
  state.buf_by_state_id[id] = bufnr
  state.cursor_by_state_id[id] = { row = row, col = col }
  return id
end

local function send_state_update(bufnr, include_file, opts)
  opts = opts or {}
  if not opts.force and not state.auto_suggest_enabled then
    return
  end
  if not state.client or not state.client:is_running() or not is_supported(bufnr) then
    return
  end

  local path = file_path(bufnr)
  if not path then
    clear_suggestion(bufnr)
    return
  end

  if cursor_is_comment_or_string(bufnr) then
    clear_suggestion(bufnr)
    return
  end

  local row, col = cursor_position()
  local updates = {}
  if include_file then
    updates[#updates + 1] = {
      kind = "file_update",
      path = path,
      content = buffer_content(bufnr),
    }
  end

  updates[#updates + 1] = {
    kind = "cursor_update",
    path = path,
    offset = cursor_offset(bufnr, row, col),
  }

  state.client:send({
    kind = "state_update",
    newId = next_state_id(bufnr, row, col),
    updates = updates,
  })
end

local function stop_pending_update(bufnr)
  local pending = state.pending_by_buf[bufnr]
  if not pending then
    return
  end

  pending.timer:stop()
  pending.timer:close()
  state.pending_by_buf[bufnr] = nil
end

local function stop_all_pending_updates()
  local buffers = {}
  for bufnr, _ in pairs(state.pending_by_buf) do
    buffers[#buffers + 1] = bufnr
  end

  for _, bufnr in ipairs(buffers) do
    stop_pending_update(bufnr)
  end
end

local function debounce_ms()
  local value = tonumber(state.config.debounce_ms) or defaults.debounce_ms
  if value < 0 then
    return defaults.debounce_ms
  end
  return value
end

local function schedule_state_update(bufnr, include_file)
  if not is_supported(bufnr) then
    return
  end

  local pending = state.pending_by_buf[bufnr]
  if pending then
    include_file = pending.include_file or include_file
    pending.timer:stop()
    pending.timer:close()
  end

  local timer = uv.new_timer()
  state.pending_by_buf[bufnr] = {
    timer = timer,
    include_file = include_file,
  }

  timer:start(debounce_ms(), 0, function()
    vim.schedule(function()
      local current = state.pending_by_buf[bufnr]
      if not current or current.timer ~= timer then
        return
      end

      stop_pending_update(bufnr)
      if vim.api.nvim_buf_is_valid(bufnr) then
        send_state_update(bufnr, current.include_file)
      end
    end)
  end)
end

local function maybe_auto_start(bufnr)
  if not state.config.auto_start or not is_supported(bufnr) then
    return
  end
  if state.client and state.client:is_running() then
    return
  end

  M.start()
end

local function on_response(message)
  local bufnr = state.buf_by_state_id[message.stateId]
  if not bufnr or not vim.api.nvim_buf_is_valid(bufnr) then
    return
  end

  if state.latest_state_id_by_buf[bufnr] ~= message.stateId then
    return
  end

  local text = {}
  for _, item in ipairs(message.items or {}) do
    if item.kind == "text" then
      text[#text + 1] = item.text or ""
    elseif item.kind == "barrier" or item.kind == "end" then
      break
    end
  end

  if #text == 0 then
    clear_suggestion(bufnr)
    return
  end

  local cursor = state.cursor_by_state_id[message.stateId]
  if not cursor then
    return
  end

  local suggestion = table.concat(text, "")
  state.suggestion_by_buf[bufnr] = {
    accepted_text = "",
    text = suggestion,
    row = cursor.row,
    col = cursor.col,
    state_id = message.stateId,
    path = file_path(bufnr),
  }
  ghost_text.show(bufnr, suggestion, cursor.row, cursor.col)
end

local function send_accept_update(suggestion, text)
  if state.client and state.client:is_running() and suggestion.path then
    state.client:send({
      kind = "accept_update",
      stateId = suggestion.state_id,
      path = suggestion.path,
      text = text,
    })
  end
end

local function next_word_chunk(text)
  if text == "" then
    return "", ""
  end

  local leading_start, leading_end = text:find("^%s+")
  local start = leading_end and leading_end + 1 or 1
  local word_start, word_end = text:find("%S+", start)
  if not word_start then
    return text, ""
  end

  local trailing_start, trailing_end = text:find("^%s+", word_end + 1)
  local chunk_end = trailing_end or word_end
  return text:sub(1, chunk_end), text:sub(chunk_end + 1)
end

local function advance_position(row, col, text)
  local lines = vim.split(text, "\n", { plain = true })
  if #lines == 1 then
    return row, col + #text
  end
  return row + #lines - 1, #lines[#lines]
end

local function on_message(message)
  if message.kind == "response" then
    on_response(message)
  elseif message.kind == "error" then
    state.last_core_error = message.message
    if state.config.suppress_expected_provider_errors and is_expected_provider_error(message.message) then
      clear_suggestion(vim.api.nvim_get_current_buf())
      return
    end
    vim.notify("inliner-core: " .. message.message, vim.log.levels.WARN)
  elseif message.kind == "health_response" then
    state.last_health = message
    local health_kind = state.pending_health_kind or "manual"
    state.pending_health_kind = nil

    if not version.is_at_least(message.coreVersion, state.config.minimum_core_version) then
      vim.notify(
        "inliner-core version "
          .. tostring(message.coreVersion or "unknown")
          .. " is older than required "
          .. tostring(state.config.minimum_core_version),
        vim.log.levels.WARN
      )
    end

    if health_kind == "startup" then
      local target = tostring(message.provider)
      if message.provider == "ollama" and message.ollamaModel and message.ollamaModel ~= "" then
        target = target .. "/" .. message.ollamaModel
      end
      local version = message.coreVersion and (" " .. message.coreVersion) or ""
      vim.notify("inliner-core" .. version .. " connected: " .. target, vim.log.levels.INFO)
    else
      vim.notify(table.concat({
        "inliner-core health",
        "version: " .. tostring(message.coreVersion or ""),
        "provider: " .. tostring(message.provider),
        "provider status: " .. tostring(message.providerStatus or ""),
        "provider reachable: " .. tostring(message.providerReachable),
        "provider error: " .. tostring(message.providerError or ""),
        "model: " .. tostring(message.ollamaModel or ""),
        "base URL: " .. tostring(message.ollamaBaseUrl or ""),
        "temperature: " .. tostring(message.ollamaTemperature or ""),
        "num predict: " .. tostring(message.ollamaNumPredict or ""),
        "timeout: " .. tostring(message.requestTimeout),
        "window bytes: " .. tostring(message.windowBytes),
        "open documents: " .. tostring(message.openDocuments),
        "in-flight requests: " .. tostring(message.inFlightRequests),
      }, "\n"), vim.log.levels.INFO)
    end
  end
end

function M.dismiss()
  local bufnr = vim.api.nvim_get_current_buf()
  local suggestion = state.suggestion_by_buf[bufnr]
  if state.client and state.client:is_running() and suggestion and suggestion.path then
    state.client:send({
      kind = "dismiss_update",
      stateId = suggestion.state_id,
      path = suggestion.path,
      text = suggestion.text,
    })
  end
  clear_suggestion(bufnr)
end

function M.accept()
  local bufnr = vim.api.nvim_get_current_buf()
  local suggestion = state.suggestion_by_buf[bufnr]
  if not suggestion or suggestion.text == "" then
    return false
  end
  if not vim.api.nvim_buf_is_valid(bufnr) then
    return false
  end

  local lines = vim.split(suggestion.text, "\n", { plain = true })
  vim.api.nvim_buf_set_text(bufnr, suggestion.row, suggestion.col, suggestion.row, suggestion.col, lines)

  local final_row = suggestion.row + #lines - 1
  local final_col = suggestion.col + #lines[1]
  if #lines > 1 then
    final_col = #lines[#lines]
  end
  vim.api.nvim_win_set_cursor(0, { final_row + 1, final_col })

  send_accept_update(suggestion, suggestion.accepted_text .. suggestion.text)

  clear_suggestion(bufnr)
  schedule_state_update(bufnr, true)

  return true
end

function M.accept_word()
  local bufnr = vim.api.nvim_get_current_buf()
  local suggestion = state.suggestion_by_buf[bufnr]
  if not suggestion or suggestion.text == "" then
    return false
  end
  if not vim.api.nvim_buf_is_valid(bufnr) then
    return false
  end

  local chunk, remaining = next_word_chunk(suggestion.text)
  if chunk == "" then
    return false
  end

  local lines = vim.split(chunk, "\n", { plain = true })
  vim.api.nvim_buf_set_text(bufnr, suggestion.row, suggestion.col, suggestion.row, suggestion.col, lines)
  local next_row, next_col = advance_position(suggestion.row, suggestion.col, chunk)
  vim.api.nvim_win_set_cursor(0, { next_row + 1, next_col })

  suggestion.accepted_text = suggestion.accepted_text .. chunk
  suggestion.text = remaining
  suggestion.row = next_row
  suggestion.col = next_col

  if remaining == "" then
    send_accept_update(suggestion, suggestion.accepted_text)
    clear_suggestion(bufnr)
    schedule_state_update(bufnr, true)
  else
    ghost_text.show(bufnr, remaining, next_row, next_col)
  end

  return true
end

local function attach_autocmds()
  if state.augroup then
    vim.api.nvim_del_augroup_by_id(state.augroup)
  end

  state.augroup = vim.api.nvim_create_augroup("Inliner", { clear = true })

  vim.api.nvim_create_autocmd({ "InsertEnter", "BufEnter" }, {
    group = state.augroup,
    callback = function(args)
      maybe_auto_start(args.buf)
      schedule_state_update(args.buf, true)
    end,
  })

  vim.api.nvim_create_autocmd("FileType", {
    group = state.augroup,
    callback = function(args)
      maybe_auto_start(args.buf)
    end,
  })

  vim.api.nvim_create_autocmd({ "TextChangedI", "TextChangedP" }, {
    group = state.augroup,
    callback = function(args)
      clear_suggestion(args.buf)
      schedule_state_update(args.buf, true)
    end,
  })

  vim.api.nvim_create_autocmd({ "CursorMovedI", "CursorMoved" }, {
    group = state.augroup,
    callback = function(args)
      clear_suggestion(args.buf)
      schedule_state_update(args.buf, false)
    end,
  })

  vim.api.nvim_create_autocmd({ "InsertLeave", "BufLeave" }, {
    group = state.augroup,
    callback = function(args)
      stop_pending_update(args.buf)
      clear_suggestion(args.buf)
    end,
  })
end

local function attach_keymaps()
  for _, mapping in ipairs(state.mapped_keys) do
    pcall(vim.keymap.del, mapping.mode, mapping.lhs)
  end
  state.mapped_keys = {}

  if state.config.accept_key ~= false and state.config.accept_key ~= nil then
    vim.keymap.set("i", state.config.accept_key, function()
      local bufnr = vim.api.nvim_get_current_buf()
      local suggestion = state.suggestion_by_buf[bufnr]
      if suggestion and suggestion.text ~= "" then
        vim.schedule(function()
          M.accept()
        end)
        return ""
      end
      return vim.api.nvim_replace_termcodes(state.config.accept_key, true, false, true)
    end, { expr = true, silent = true, desc = "Accept inliner suggestion" })
    state.mapped_keys[#state.mapped_keys + 1] = { mode = "i", lhs = state.config.accept_key }
  end

  if state.config.accept_word_key ~= false and state.config.accept_word_key ~= nil then
    vim.keymap.set("i", state.config.accept_word_key, function()
      local bufnr = vim.api.nvim_get_current_buf()
      local suggestion = state.suggestion_by_buf[bufnr]
      if suggestion and suggestion.text ~= "" then
        vim.schedule(function()
          M.accept_word()
        end)
        return ""
      end
      return vim.api.nvim_replace_termcodes(state.config.accept_word_key, true, false, true)
    end, { expr = true, silent = true, desc = "Accept next inliner word" })
    state.mapped_keys[#state.mapped_keys + 1] = { mode = "i", lhs = state.config.accept_word_key }
  end

  if state.config.dismiss_key ~= false and state.config.dismiss_key ~= nil then
    vim.keymap.set({ "i", "n" }, state.config.dismiss_key, function()
      M.dismiss()
    end, { silent = true, desc = "Dismiss inliner suggestion" })
    state.mapped_keys[#state.mapped_keys + 1] = { mode = "i", lhs = state.config.dismiss_key }
    state.mapped_keys[#state.mapped_keys + 1] = { mode = "n", lhs = state.config.dismiss_key }
  end

  if state.config.complete_key ~= false and state.config.complete_key ~= nil then
    vim.keymap.set({ "i", "n" }, state.config.complete_key, function()
      M.complete()
    end, { silent = true, desc = "Request inliner completion" })
    state.mapped_keys[#state.mapped_keys + 1] = { mode = "i", lhs = state.config.complete_key }
    state.mapped_keys[#state.mapped_keys + 1] = { mode = "n", lhs = state.config.complete_key }
  end
end

function M.setup(opts)
  stop_all_pending_updates()
  state.config = vim.tbl_deep_extend("force", defaults, opts or {})
  state.selected_model = state.config.ollama_model
  state.auto_suggest_enabled = state.config.auto_suggest ~= false
  attach_autocmds()
  attach_keymaps()
end

function M.enable()
  state.auto_suggest_enabled = true
  vim.notify("inliner automatic suggestions enabled", vim.log.levels.INFO)
end

function M.disable()
  state.auto_suggest_enabled = false
  stop_all_pending_updates()
  clear_suggestion(vim.api.nvim_get_current_buf())
  vim.notify("inliner automatic suggestions disabled", vim.log.levels.INFO)
end

function M.toggle()
  if state.auto_suggest_enabled then
    M.disable()
  else
    M.enable()
  end
end

function M.debug_dir()
  return debug_dir()
end

function M.open_debug_dir()
  vim.fn.mkdir(debug_dir(), "p")
  return open_path(debug_dir())
end

function M.open_timing_log()
  return open_path(vim.fs.joinpath(debug_dir(), "completion-timings.log"))
end

function M.open_telemetry_log()
  return open_path(vim.fs.joinpath(debug_dir(), "request-lifecycle.jsonl"))
end

function M.open_latest_prompt()
  local path = latest_file(vim.fs.joinpath(debug_dir(), "prompts", "*.prompt.txt"))
  if not path then
    vim.notify("no inliner prompt logs found in " .. vim.fs.joinpath(debug_dir(), "prompts"), vim.log.levels.WARN)
    return false
  end
  return open_path(path)
end

function M.toggle_debug()
  state.config.debug_verbose = not state.config.debug_verbose
  local status = state.config.debug_verbose and "enabled" or "disabled"
  local suffix = ""
  if state.client and state.client:is_running() then
    suffix = "; restart inliner-core for this to affect prompt/timing logs"
  end
  vim.notify("inliner debug logging " .. status .. suffix, vim.log.levels.INFO)
  return state.config.debug_verbose
end

function M.status()
  return {
    running = state.client ~= nil and state.client:is_running(),
    auto_suggest_enabled = state.auto_suggest_enabled,
    debug_verbose = state.config.debug_verbose == true,
    debug_dir = debug_dir(),
    ollama_base_url = ollama_base_url(),
    selected_model = selected_model(),
    last_core_error = state.last_core_error,
    last_health = state.last_health,
    suggestions = vim.tbl_count(state.suggestion_by_buf),
    pending = vim.tbl_count(state.pending_by_buf),
  }
end

function M.debug_state()
  return M.status()
end

function M.switch_model(model)
  model = vim.trim(model or "")
  if model == "" then
    vim.notify("model name is required", vim.log.levels.WARN)
    return false
  end

  state.selected_model = model
  state.config.ollama_model = model
  local was_running = state.client ~= nil and state.client:is_running()
  if was_running then
    M.stop()
    M.start()
  end
  local suffix = was_running and " and restarted inliner-core" or ""
  vim.notify("inliner Ollama model set to " .. model .. suffix, vim.log.levels.INFO)
  return true
end

function M.list_models(callback)
  local url = ollama_base_url() .. "/api/tags"
  local handle = vim.system({ "curl", "-fsS", url }, { text = true }, function(result)
    local models, err = parse_ollama_models(result.stdout)
    vim.schedule(function()
      if result.code ~= 0 then
        local message = vim.trim(result.stderr or "")
        if message == "" then
          message = "curl exited with code " .. tostring(result.code)
        end
        if callback then
          callback(nil, message)
        else
          vim.notify("failed to list Ollama models: " .. message, vim.log.levels.WARN)
        end
        return
      end
      if err then
        if callback then
          callback(nil, err)
        else
          vim.notify(err, vim.log.levels.WARN)
        end
        return
      end
      state.cached_models = models
      if callback then
        callback(models, nil)
        return
      end
      if #models == 0 then
        vim.notify("no Ollama models found at " .. ollama_base_url(), vim.log.levels.INFO)
        return
      end
      local lines = { "Ollama models at " .. ollama_base_url() .. ":" }
      for _, model in ipairs(models) do
        local marker = model.name == selected_model() and "* " or "- "
        lines[#lines + 1] = marker .. model.name .. " (" .. format_bytes(model.size) .. ")"
      end
      vim.notify(table.concat(lines, "\n"), vim.log.levels.INFO)
    end)
  end)
  return handle ~= nil
end

function M.pick_model()
  return M.list_models(function(models, err)
    if err then
      vim.notify("failed to list Ollama models: " .. err, vim.log.levels.WARN)
      return
    end
    if not models or #models == 0 then
      vim.notify("no Ollama models found at " .. ollama_base_url(), vim.log.levels.INFO)
      return
    end
    state.cached_models = models
    vim.ui.select(models, {
      prompt = "Inliner Ollama model",
      format_item = function(model)
        return model.name .. " (" .. format_bytes(model.size) .. ")"
      end,
    }, function(choice)
      if choice then
        M.switch_model(choice.name)
      end
    end)
  end)
end

function M.model_info()
  local health = state.last_health or {}
  local lines = {
    "inliner model/resource information",
    "provider: " .. tostring(health.provider or ""),
    "provider status: " .. tostring(health.providerStatus or ""),
    "provider reachable: " .. tostring(health.providerReachable),
    "provider error: " .. tostring(health.providerError or ""),
    "selected model: " .. tostring(selected_model() or health.ollamaModel or ""),
    "health model: " .. tostring(health.ollamaModel or ""),
    "base URL: " .. tostring(health.ollamaBaseUrl or ollama_base_url()),
    "temperature: " .. tostring(health.ollamaTemperature or ""),
    "num predict: " .. tostring(health.ollamaNumPredict or ""),
    "timeout: " .. tostring(health.requestTimeout or ""),
    "window bytes: " .. tostring(health.windowBytes or ""),
    "open documents: " .. tostring(health.openDocuments or ""),
    "in-flight requests: " .. tostring(health.inFlightRequests or ""),
  }
  vim.notify(table.concat(lines, "\n"), vim.log.levels.INFO)
  return lines
end

function M.test_completion()
  return M.complete()
end

function M._complete_model_for_command(arglead)
  local matches = {}
  arglead = arglead or ""
  for _, model in ipairs(state.cached_models or {}) do
    if model.name:sub(1, #arglead) == arglead then
      matches[#matches + 1] = model.name
    end
  end
  local current = selected_model()
  if current and current:sub(1, #arglead) == arglead then
    matches[#matches + 1] = current
  end
  table.sort(matches)
  return matches
end

function M.start()
  if state.client and state.client:is_running() then
    return
  end

  local cmd, cwd = state.config.cmd, state.config.cwd
  if not cmd then
    cmd, cwd = default_cmd()
  end

  state.client = client.new({
    cmd = cmd,
    cwd = cwd,
    env = core_env(),
    on_message = on_message,
  })

  state.client:start()
  state.client:send({ kind = "greeting", allowGitignore = state.config.allow_gitignore })
  state.pending_health_kind = "startup"
  state.client:send({ kind = "health_request" })
end

function M.stop()
  stop_all_pending_updates()
  local buffers = {}
  for bufnr, _ in pairs(state.suggestion_by_buf) do
    buffers[#buffers + 1] = bufnr
  end
  for _, bufnr in ipairs(buffers) do
    clear_suggestion(bufnr)
  end
  if state.client then
    state.client:stop()
    state.client = nil
  end
end

function M.health()
  if not state.client or not state.client:is_running() then
    vim.notify("inliner-core is not running", vim.log.levels.WARN)
    return
  end

  state.pending_health_kind = "manual"
  state.client:send({ kind = "health_request" })
end

function M.complete()
  local bufnr = vim.api.nvim_get_current_buf()
  if not is_supported(bufnr) then
    return false
  end

  if not state.client or not state.client:is_running() then
    if state.config.auto_start then
      M.start()
    else
      vim.notify("inliner-core is not running; run :InlinerStart", vim.log.levels.WARN)
      return false
    end
  end

  stop_pending_update(bufnr)
  clear_suggestion(bufnr)
  send_state_update(bufnr, true, { force = true })
  return true
end

function M._set_suggestion_for_test(bufnr, suggestion)
  state.suggestion_by_buf[bufnr] = suggestion
  ghost_text.show(bufnr, suggestion.text, suggestion.row, suggestion.col)
end

function M._cursor_is_comment_or_string_for_test(bufnr)
  return cursor_is_comment_or_string(bufnr)
end

function M._parse_ollama_models_for_test(payload)
  return parse_ollama_models(payload)
end

function M._format_bytes_for_test(value)
  return format_bytes(value)
end

M.setup()

return M
