local ghost_text = require("inliner.ghost_text")

local bufnr = vim.api.nvim_create_buf(false, true)
vim.api.nvim_set_current_buf(bufnr)
vim.api.nvim_buf_set_lines(bufnr, 0, -1, false, { "package main", "" })

ghost_text.show(bufnr, "completion", 0, 7)

local marks = vim.api.nvim_buf_get_extmarks(bufnr, ghost_text.namespace, 0, -1, { details = true })
assert(#marks == 1, "single-line ghost text should create one extmark")
assert(marks[1][4].virt_text[1][1] == "completion", "single-line virt_text should contain completion")
assert(marks[1][4].virt_lines == nil, "single-line ghost text should not create virt_lines")

ghost_text.show(bufnr, "first\nsecond\nthird", 0, 7)

marks = vim.api.nvim_buf_get_extmarks(bufnr, ghost_text.namespace, 0, -1, { details = true })
assert(#marks == 1, "multiline ghost text should create one extmark")
assert(marks[1][4].virt_text[1][1] == "first", "first line should render inline")
assert(marks[1][4].virt_lines[1][1][1] == "second", "second line should render as virtual line")
assert(marks[1][4].virt_lines[2][1][1] == "third", "third line should render as virtual line")

ghost_text.clear(bufnr)
marks = vim.api.nvim_buf_get_extmarks(bufnr, ghost_text.namespace, 0, -1, { details = true })
assert(#marks == 0, "clear should remove ghost text extmarks")
