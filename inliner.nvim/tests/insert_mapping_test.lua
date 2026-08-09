local inliner = require("inliner")

inliner.setup({
  accept_key = "\\<Tab>",
  accept_word_key = "\\1",
  auto_start = false,
  auto_suggest = false,
  dismiss_key = false,
})

local bufnr = vim.api.nvim_create_buf(false, true)
vim.api.nvim_set_current_buf(bufnr)
vim.api.nvim_buf_set_lines(bufnr, 0, -1, false, { "foo := " })
vim.bo[bufnr].filetype = "go"
vim.api.nvim_win_set_cursor(0, { 1, 7 })

inliner._set_suggestion_for_test(bufnr, {
  accepted_text = "",
  text = "alpha beta",
  row = 0,
  col = 7,
  state_id = "1",
  path = "/tmp/main.go",
})

local mapping = vim.fn.maparg("\\1", "i", false, true)
assert(type(mapping.callback) == "function", "accept_word mapping should expose a Lua callback")
assert(mapping.callback() == "", "accept_word mapping should swallow mapped keys when suggestion exists")
vim.wait(100, function()
  return vim.api.nvim_buf_get_lines(bufnr, 0, 1, false)[1] == "foo := alpha "
end)

assert(vim.api.nvim_buf_get_lines(bufnr, 0, 1, false)[1] == "foo := alpha ", "accept_word mapping should insert one word")
