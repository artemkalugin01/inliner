local M = {}

local namespace = vim.api.nvim_create_namespace("inliner.ghost_text")

M.namespace = namespace

function M.clear(bufnr)
  if vim.api.nvim_buf_is_valid(bufnr) then
    vim.api.nvim_buf_clear_namespace(bufnr, namespace, 0, -1)
  end
end

function M.show(bufnr, text, row, col)
  if text == "" or not vim.api.nvim_buf_is_valid(bufnr) then
    M.clear(bufnr)
    return
  end

  local lines = vim.split(text, "\n", { plain = true })
  local first_line = lines[1] or ""
  local virt_lines = nil

  if #lines > 1 then
    virt_lines = {}
    for i = 2, #lines do
      virt_lines[#virt_lines + 1] = { { lines[i], "Comment" } }
    end
  end

  M.clear(bufnr)
  vim.api.nvim_buf_set_extmark(bufnr, namespace, row, col, {
    virt_text = { { first_line, "Comment" } },
    virt_text_pos = "inline",
    virt_lines = virt_lines,
    hl_mode = "combine",
  })
end

return M
