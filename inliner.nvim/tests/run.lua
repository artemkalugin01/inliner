local source = debug.getinfo(1, "S").source:sub(2)
local tests_dir = vim.fs.dirname(source)
local plugin_root = vim.fs.dirname(tests_dir)

vim.opt.runtimepath:prepend(plugin_root)
vim.notify = function() end

local tests = {
  "version_test.lua",
  "client_test.lua",
  "accept_word_test.lua",
  "insert_mapping_test.lua",
  "ghost_text_test.lua",
  "plugin_test.lua",
  "context_suppression_test.lua",
  "commands_test.lua",
}

local failures = {}

for _, test_file in ipairs(tests) do
  local path = vim.fs.joinpath(tests_dir, test_file)
  local ok, err = xpcall(function()
    dofile(path)
  end, debug.traceback)

  if ok then
    print("ok " .. test_file)
  else
    failures[#failures + 1] = test_file .. "\n" .. err
    print("not ok " .. test_file)
  end
end

if #failures > 0 then
  print(table.concat(failures, "\n\n"))
  os.exit(1)
end

print("all inliner.nvim tests passed")
