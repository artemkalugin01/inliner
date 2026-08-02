if vim.g.loaded_inliner == 1 then
  return
end

vim.g.loaded_inliner = 1

vim.api.nvim_create_user_command("InlinerStart", function()
  require("inliner").start()
end, {})

vim.api.nvim_create_user_command("InlinerStop", function()
  require("inliner").stop()
end, {})

vim.api.nvim_create_user_command("InlinerAccept", function()
  require("inliner").accept()
end, {})

vim.api.nvim_create_user_command("InlinerAcceptWord", function()
  require("inliner").accept_word()
end, {})

vim.api.nvim_create_user_command("InlinerDismiss", function()
  require("inliner").dismiss()
end, {})

vim.api.nvim_create_user_command("InlinerHealth", function()
  require("inliner").health()
end, {})

vim.api.nvim_create_user_command("InlinerComplete", function()
  require("inliner").complete()
end, {})

vim.api.nvim_create_user_command("InlinerEnable", function()
  require("inliner").enable()
end, {})

vim.api.nvim_create_user_command("InlinerDisable", function()
  require("inliner").disable()
end, {})

vim.api.nvim_create_user_command("InlinerToggle", function()
  require("inliner").toggle()
end, {})
