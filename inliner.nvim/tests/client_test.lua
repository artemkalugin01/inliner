local client = require("inliner.client")

local received = {}
local c = client.new({
  cmd = { "unused" },
  on_message = function(message)
    received[#received + 1] = message
  end,
})

c:handle_stdout({ 'INLINER-MESSAGE {"kind":"health_response",' })
c:handle_stdout({ '"provider":"fake"}', '' })
vim.wait(100, function()
  return #received == 1
end)

assert(#received == 1, "expected one decoded message")
assert(received[1].kind == "health_response", "expected health_response kind")
assert(received[1].provider == "fake", "expected fake provider")

c:handle_stdout({ "non-protocol log line", "" })
vim.wait(20, function()
  return false
end)
assert(#received == 1, "non-protocol output should be ignored")

c:handle_stdout({ "INLINER-MESSAGE not-json", "" })
vim.wait(20, function()
  return false
end)
assert(#received == 1, "invalid JSON should not call on_message")
