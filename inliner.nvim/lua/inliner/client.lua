local M = {}

local message_prefix = "INLINER%-MESSAGE "

local Client = {}
Client.__index = Client

function M.new(opts)
  return setmetatable({
    cmd = opts.cmd,
    cwd = opts.cwd,
    env = opts.env,
    on_message = opts.on_message,
    job_id = nil,
    stdout_buffer = "",
  }, Client)
end

function Client:is_running()
  return self.job_id ~= nil
end

function Client:start()
  if self.job_id then
    return
  end

  self.job_id = vim.fn.jobstart(self.cmd, {
    cwd = self.cwd,
    env = self.env,
    stdin = "pipe",
    stdout_buffered = false,
    stderr_buffered = false,
    on_stdout = function(_, data)
      self:handle_stdout(data)
    end,
    on_stderr = function(_, data)
      for _, line in ipairs(data or {}) do
        if line ~= "" then
          vim.schedule(function()
            vim.notify("inliner-core stderr: " .. line, vim.log.levels.WARN)
          end)
        end
      end
    end,
    on_exit = function(_, code)
      self.job_id = nil
      if self.stopping then
        self.stopping = false
        return
      end
      if code ~= 0 then
        vim.schedule(function()
          vim.notify("inliner-core exited with code " .. code, vim.log.levels.WARN)
        end)
      end
    end,
  })

  if self.job_id <= 0 then
    local err = "failed to start inliner-core"
    self.job_id = nil
    error(err)
  end
end

function Client:stop()
  if not self.job_id then
    return
  end

  self:send({ kind = "shutdown" })
  self.stopping = true
  vim.fn.jobstop(self.job_id)
  self.job_id = nil
end

function Client:send(message)
  if not self.job_id then
    return
  end

  vim.fn.chansend(self.job_id, vim.json.encode(message) .. "\n")
end

function Client:handle_stdout(data)
  if not data or #data == 0 then
    return
  end

  self.stdout_buffer = self.stdout_buffer .. data[1]

  for i = 2, #data do
    self:handle_line(self.stdout_buffer)
    self.stdout_buffer = data[i]
  end
end

function Client:handle_line(line)
  local payload = line:match("^" .. message_prefix .. "(.+)$")
  if not payload then
    return
  end

  local ok, message = pcall(vim.json.decode, payload)
  if not ok then
    vim.schedule(function()
      vim.notify("failed to decode inliner-core message", vim.log.levels.WARN)
    end)
    return
  end

  if self.on_message then
    vim.schedule(function()
      self.on_message(message)
    end)
  end
end

return M
