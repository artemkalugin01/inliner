local inliner = require("inliner")

inliner.setup({
  accept_key = false,
  accept_word_key = false,
  auto_suggest = true,
  complete_key = false,
  dismiss_key = false,
})

local bufnr = vim.api.nvim_create_buf(true, false)
vim.api.nvim_set_current_buf(bufnr)
vim.api.nvim_buf_set_name(bufnr, "/tmp/inliner-state-update.go")
vim.bo[bufnr].filetype = "go"
vim.api.nvim_buf_set_lines(bufnr, 0, -1, true, {
  "package main",
  "",
  "func main() {}",
})
vim.api.nvim_win_set_cursor(0, { 1, 0 })

local messages = {}
inliner._set_client_for_test({
  is_running = function()
    return true
  end,
  send = function(_, message)
    messages[#messages + 1] = message
  end,
  stop = function() end,
})

inliner._send_state_update_for_test(bufnr, false)
assert(#messages == 1, "first cursor-only update should send a message")
assert(messages[1].updates[1].kind == "file_update", "first update should include file content")
assert(messages[1].updates[1].content:find("package main", 1, true) ~= nil, "file update should include buffer content")
assert(messages[1].updates[2].kind == "cursor_update", "first update should include cursor after file content")

messages = {}
inliner._send_state_update_for_test(bufnr, false)
assert(#messages == 1, "second cursor-only update should send a message")
assert(#messages[1].updates == 1, "second cursor-only update should stay lightweight")
assert(messages[1].updates[1].kind == "cursor_update", "second update should only include cursor")

inliner.stop()
