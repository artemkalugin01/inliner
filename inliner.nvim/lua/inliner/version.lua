local M = {}

local function parse(version)
  local major, minor, patch = tostring(version or ""):match("^(%d+)%.(%d+)%.(%d+)")
  if not major then
    return nil
  end

  return {
    major = tonumber(major),
    minor = tonumber(minor),
    patch = tonumber(patch),
  }
end

function M.is_at_least(version, minimum)
  local current = parse(version)
  local required = parse(minimum)
  if not current or not required then
    return false
  end

  for _, field in ipairs({ "major", "minor", "patch" }) do
    if current[field] > required[field] then
      return true
    end
    if current[field] < required[field] then
      return false
    end
  end

  return true
end

return M
