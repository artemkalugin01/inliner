local inliner = require("inliner")

inliner.setup({
  accept_key = false,
  auto_start = false,
  auto_suggest = false,
  complete_key = "<C-Space>",
  debug_dir = "/tmp/inliner.nvim-test-debug",
  debug_verbose = false,
  debounce_ms = 1,
  dismiss_key = false,
  minimum_core_version = "0.1.0",
  ollama_base_url = "http://127.0.0.1:11434/",
  ollama_model = "qwen2.5-coder:7b",
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
assert(inliner.debug_dir() == "/tmp/inliner.nvim-test-debug", "debug_dir should return configured path")
assert(inliner.toggle_debug() == true, "toggle_debug should enable debug logging")
local status = inliner.status()
assert(status.running == false, "status should report stopped core")
assert(status.debug_verbose == true, "status should include debug flag")
assert(status.debug_dir == "/tmp/inliner.nvim-test-debug", "status should include debug dir")
assert(status.ollama_base_url == "http://127.0.0.1:11434", "status should include normalized Ollama base URL")
assert(status.selected_model == "qwen2.5-coder:7b", "status should include selected model")
assert(inliner.switch_model("deepseek-coder:latest") == true, "switch_model should accept model name")
assert(inliner.status().selected_model == "deepseek-coder:latest", "switch_model should update selected model")
assert(inliner.switch_model(" ") == false, "switch_model should reject blank model")
assert(inliner.test_completion() == false, "test_completion should use manual completion path")
inliner.dismiss()
assert(inliner.accept() == false, "accept should return false without a visible suggestion")

local models, err = inliner._parse_ollama_models_for_test([[{"models":[{"name":"z:latest","size":1024},{"name":"a:latest","size":1073741824}]}]])
assert(err == nil, "model parser should not fail")
assert(#models == 2 and models[1].name == "a:latest" and models[2].name == "z:latest", "models should be sorted by name")
assert(inliner._format_bytes_for_test(1073741824) == "1.0 GiB", "format_bytes should format GiB")

inliner.stop()
