local inliner = require("inliner")
local ghost_text = require("inliner.ghost_text")

inliner.setup({
  accept_key = false,
  accept_word_key = "<M-]>",
  auto_start = false,
  dismiss_key = false,
})

assert(vim.fn.maparg("<M-]>", "i") ~= "", "accept_word_key should create insert mapping")

local bufnr = vim.api.nvim_create_buf(false, true)
vim.api.nvim_set_current_buf(bufnr)
vim.api.nvim_buf_set_lines(bufnr, 0, -1, false, { "foo := " })
vim.bo[bufnr].filetype = "go"

inliner._set_suggestion_for_test(bufnr, {
  accepted_text = "",
  text = "alpha beta gamma delta epsilon",
  row = 0,
  col = 7,
  state_id = "1",
  path = "/tmp/main.go",
})

assert(inliner.accept_word() == true, "first word accept should succeed")
assert(vim.api.nvim_buf_get_lines(bufnr, 0, 1, false)[1] == "foo := alpha ", "first word should be inserted")

local marks = vim.api.nvim_buf_get_extmarks(bufnr, ghost_text.namespace, 0, -1, { details = true })
assert(marks[1][4].virt_text[1][1] == "beta gamma delta epsilon", "remaining words should stay visible")

assert(inliner.accept_word() == true, "second word accept should succeed")
assert(inliner.accept_word() == true, "third word accept should succeed")
assert(vim.api.nvim_buf_get_lines(bufnr, 0, 1, false)[1] == "foo := alpha beta gamma ", "three words should be inserted")

marks = vim.api.nvim_buf_get_extmarks(bufnr, ghost_text.namespace, 0, -1, { details = true })
assert(marks[1][4].virt_text[1][1] == "delta epsilon", "two words should remain visible")

assert(inliner.accept() == true, "full accept should accept remaining suggestion")
assert(vim.api.nvim_buf_get_lines(bufnr, 0, 1, false)[1] == "foo := alpha beta gamma delta epsilon", "full accept should finish suggestion")

marks = vim.api.nvim_buf_get_extmarks(bufnr, ghost_text.namespace, 0, -1, { details = true })
assert(#marks == 0, "accept should clear ghost text")
