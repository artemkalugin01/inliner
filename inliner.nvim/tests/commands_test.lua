local plugin_root = vim.fs.dirname(vim.fs.dirname(debug.getinfo(1, "S").source:sub(2)))

vim.g.loaded_inliner = nil
dofile(vim.fs.joinpath(plugin_root, "plugin", "inliner.lua"))

for _, name in ipairs({
  "InlinerStart",
  "InlinerStop",
  "InlinerHealth",
  "InlinerComplete",
  "InlinerOpenDebugDir",
  "InlinerOpenTimingLog",
  "InlinerOpenTelemetryLog",
  "InlinerOpenLatestPrompt",
  "InlinerToggleDebug",
  "InlinerStatus",
  "InlinerListModels",
  "InlinerPickModel",
  "InlinerSwitchModel",
  "InlinerTestCompletion",
  "InlinerModelInfo",
}) do
  assert(vim.fn.exists(":" .. name) == 2, name .. " command should exist")
end
