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
  dismiss_key = "<C-]>",
  debounce_ms = 120,
  filetypes = { go = true },
  minimum_core_version = "0.1.0",
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
    vim.notify("inliner-core: " .. message.message, vim.log.levels.WARN)
  elseif message.kind == "health_response" then
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
      if M.accept() then
        return ""
      end
      return vim.api.nvim_replace_termcodes(state.config.accept_key, true, false, true)
    end, { expr = true, silent = true, desc = "Accept inliner suggestion" })
    state.mapped_keys[#state.mapped_keys + 1] = { mode = "i", lhs = state.config.accept_key }
  end

  if state.config.accept_word_key ~= false and state.config.accept_word_key ~= nil then
    vim.keymap.set("i", state.config.accept_word_key, function()
      if M.accept_word() then
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

M.setup()

return M
