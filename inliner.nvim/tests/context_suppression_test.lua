local inliner = require("inliner")

inliner.setup({
  accept_key = false,
  auto_start = false,
  auto_suggest = false,
  dismiss_key = false,
  suppress_in_comments_strings = true,
})

vim.cmd("syntax on")
vim.cmd("syntax clear")
vim.cmd([[syntax match inlinerTestComment "//.*$"]])
vim.cmd([[syntax region inlinerTestString start='"' end='"']])

local bufnr = vim.api.nvim_get_current_buf()
vim.api.nvim_buf_set_lines(bufnr, 0, -1, true, {
  "package main",
  "// comment text",
  'var message = "hello"',
  "func main() {}",
})

vim.api.nvim_win_set_cursor(0, { 2, 4 })
assert(inliner._cursor_is_comment_or_string_for_test(bufnr) == true, "cursor in comment should suppress")

vim.api.nvim_win_set_cursor(0, { 3, 16 })
assert(inliner._cursor_is_comment_or_string_for_test(bufnr) == true, "cursor in string should suppress")

vim.api.nvim_win_set_cursor(0, { 4, 2 })
assert(inliner._cursor_is_comment_or_string_for_test(bufnr) == false, "cursor in code should not suppress")

inliner.setup({
  accept_key = false,
  auto_start = false,
  auto_suggest = false,
  dismiss_key = false,
  suppress_in_comments_strings = false,
})
vim.api.nvim_win_set_cursor(0, { 2, 4 })
assert(inliner._cursor_is_comment_or_string_for_test(bufnr) == false, "disabled suppression should not suppress")
