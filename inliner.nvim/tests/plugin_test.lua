local inliner = require("inliner")

inliner.setup({
  accept_key = false,
  auto_start = false,
  auto_suggest = false,
  complete_key = "<C-Space>",
  debounce_ms = 1,
  dismiss_key = false,
  minimum_core_version = "0.1.0",
})

assert(vim.fn.maparg("<C-Space>", "i") ~= "", "complete_key should create insert mapping")
assert(vim.fn.maparg("<Tab>", "i") == "", "disabled accept_key should not create tab mapping")

vim.bo.filetype = "lua"
assert(inliner.complete() == false, "manual completion should ignore unsupported filetypes")

vim.bo.filetype = "go"
assert(inliner.complete() == false, "manual completion should fail when core is stopped and auto_start is false")

inliner.enable()
inliner.disable()
inliner.toggle()
inliner.toggle()
inliner.dismiss()
assert(inliner.accept() == false, "accept should return false without a visible suggestion")

inliner.stop()
