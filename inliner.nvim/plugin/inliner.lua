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

vim.api.nvim_create_user_command("InlinerOpenDebugDir", function()
  require("inliner").open_debug_dir()
end, {})

vim.api.nvim_create_user_command("InlinerOpenTimingLog", function()
  require("inliner").open_timing_log()
end, {})

vim.api.nvim_create_user_command("InlinerOpenTelemetryLog", function()
  require("inliner").open_telemetry_log()
end, {})

vim.api.nvim_create_user_command("InlinerOpenLatestPrompt", function()
  require("inliner").open_latest_prompt()
end, {})

vim.api.nvim_create_user_command("InlinerToggleDebug", function()
  require("inliner").toggle_debug()
end, {})

vim.api.nvim_create_user_command("InlinerStatus", function()
  vim.print(require("inliner").status())
end, {})

vim.api.nvim_create_user_command("InlinerListModels", function()
  require("inliner").list_models()
end, {})

vim.api.nvim_create_user_command("InlinerPickModel", function()
  require("inliner").pick_model()
end, {})

vim.api.nvim_create_user_command("InlinerSwitchModel", function(args)
  require("inliner").switch_model(args.args)
end, { nargs = 1, complete = "customlist,v:lua.require'inliner'._complete_model_for_command" })

vim.api.nvim_create_user_command("InlinerTestCompletion", function()
  require("inliner").test_completion()
end, {})

vim.api.nvim_create_user_command("InlinerModelInfo", function()
  local inliner = require("inliner")
  if inliner.status().running then
    inliner.health()
  end
  inliner.model_info()
end, {})
