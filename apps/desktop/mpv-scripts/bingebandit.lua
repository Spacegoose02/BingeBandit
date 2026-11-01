-- BingeBandit's own player controls, drawn over mpv in the Windows app.
--
-- The app (src/mpv.js) starts mpv with this script and mpv's own controls
-- turned off. The script draws the title, the progress bar, the skip and up
-- next prompts, messages, and an options menu (the page's entries, such as
-- channels or Watch Together, then audio and subtitles), all sized for a TV
-- across the room and driven with a remote, the keyboard or a mouse.
--
-- From the app (script-message-to bingebandit <name> <json or text>):
--   bb-state  {mode, title, subtitle, markers, prompt, upNext, badge}
--   bb-menu   [{title, items: [{id, label, active, disabled}]}]
--   bb-toast  text
-- To the app (script-message, seen as client-message on the IPC pipe):
--   bb-enter, bb-next, bb-pick <id>, bb-channel up|down
--
-- input.conf routes the keys here with `script-binding bingebandit/<name>`;
-- the same names have default keys below for an input.conf that doesn't.
-- Written for Lua 5.1 (LuaJIT, as in mpv's Windows builds) and 5.2.

local mp = require 'mp'
local utils = require 'mp.utils'

local ACCENT = '2dd4bf'
local INK = '04201c'
local FONT = 'Inter'
local HIDE_AFTER = 4 -- seconds without input before the controls fade out

-- What the app told us.
local state = { mode = 'vod', title = '', subtitle = '', markers = {}, prompt = '', upNext = '', badge = '' }
local page_menu = {}

-- What mpv tells us.
local prop = {}
for _, name in ipairs({ 'time-pos', 'duration', 'pause', 'paused-for-cache', 'seeking', 'idle-active', 'volume', 'mute',
  'aid', 'sid', 'media-title' }) do
  mp.observe_property(name, 'native', function(_, v)
    prop[name] = v
    if name == 'pause' or name == 'paused-for-cache' then poke(false) end
  end)
end
mp.observe_property('track-list', 'native', function(_, v) prop['track-list'] = v or {} end)
mp.observe_property('demuxer-cache-state', 'native', function(_, v) prop.cache = v end)
mp.observe_property('osd-dimensions', 'native', function(_, v)
  prop.osd = v
  render()
end)

local shown_until = 0
local menu = nil -- { rows = {...}, focus = n, scroll = px }
local toast = { text = '', till = 0 }
local hits = {} -- clickable areas of the last frame, in script pixels
local W, H = 1920, 1080
local overlay = mp.create_osd_overlay('ass-events')
local timer

-- Drawing ---------------------------------------------------------------

local function bgr(rgb) return rgb:sub(5, 6) .. rgb:sub(3, 4) .. rgb:sub(1, 2) end

local function esc(s)
  s = tostring(s or '')
  s = s:gsub('\\', '\\\239\187\160'):gsub('{', '\\{'):gsub('}', '\\}'):gsub('\n', ' ')
  return s
end

local function clip_chars(s, n)
  s = tostring(s or '')
  -- Count characters, not bytes (UTF-8 continuation bytes are 0x80-0xBF).
  local count, cut = 0, nil
  for i = 1, #s do
    local b = s:byte(i)
    if b < 0x80 or b >= 0xC0 then
      count = count + 1
      if count == n then cut = i end
    end
  end
  if count <= n then return s end
  return s:sub(1, cut - 1) .. '…'
end

local function fmt_time(t)
  if not t or t < 0 then t = 0 end
  t = math.floor(t)
  local h, m, s = math.floor(t / 3600), math.floor(t / 60) % 60, t % 60
  if h > 0 then return string.format('%d:%02d:%02d', h, m, s) end
  return string.format('%d:%02d', m, s)
end

local function rr_path(x0, y0, x1, y1, r)
  r = math.min(r, (x1 - x0) / 2, (y1 - y0) / 2)
  local k = r * 0.45 -- bezier handle that looks round
  return string.format(
    'm %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d',
    x0 + r, y0, x1 - r, y0, x1 - k, y0, x1, y0 + k, x1, y0 + r,
    x1, y1 - r, x1, y1 - k, x1 - k, y1, x1 - r, y1,
    x0 + r, y1, x0 + k, y1, x0, y1 - k, x0, y1 - r,
    x0, y0 + r, x0, y0 + k, x0 + k, y0, x0 + r, y0)
end

local out = {}

-- A filled shape. alpha: 0 opaque to 255 clear.
local function shape(path, rgb, alpha, clip)
  out[#out + 1] = string.format('{\\an7\\pos(0,0)\\bord0\\shad0\\1c&H%s&\\1a&H%02X&%s\\p1}%s{\\p0}',
    bgr(rgb), alpha or 0, clip or '', path)
end

local function box(x0, y0, x1, y1, r, rgb, alpha, clip)
  if r and r > 0 then shape(rr_path(x0, y0, x1, y1, r), rgb, alpha, clip)
  else shape(string.format('m %d %d l %d %d l %d %d l %d %d', x0, y0, x1, y0, x1, y1, x0, y1), rgb, alpha, clip) end
end

local function text(x, y, an, size, s, rgb, alpha, bold, clip)
  out[#out + 1] = string.format('{\\an%d\\pos(%d,%d)\\fn%s\\fs%d\\b%d\\q2\\bord0\\shad1\\4c&H000000&\\4a&HA0&\\1c&H%s&\\1a&H%02X&%s}%s',
    an, x, y, FONT, size, bold and 1 or 0, bgr(rgb or 'ffffff'), alpha or 0, clip or '', esc(s))
end

local function hit(x0, y0, x1, y1, fn) hits[#hits + 1] = { x0, y0, x1, y1, fn } end

-- Darkens the top or bottom of the picture so text reads over anything.
local function shade(y0, y1, from_top)
  local bands = 12
  for i = 0, bands - 1 do
    local a = from_top and i or (bands - 1 - i)
    local ya = y0 + (y1 - y0) * i / bands
    local yb = y0 + (y1 - y0) * (i + 1) / bands
    box(0, math.floor(ya), W, math.ceil(yb), 0, '000000', 0x50 + a * 14)
  end
end

local function icon_play(x, y, s, rgb)
  shape(string.format('m %d %d l %d %d l %d %d', x, y, x + s, y + s / 2, x, y + s), rgb)
end

local function icon_pause(x, y, s, rgb)
  local w = math.floor(s * 0.32)
  box(x, y, x + w, y + s, 3, rgb)
  box(x + s - w, y, x + s, y + s, 3, rgb)
end

local function icon_back(x, y, s, rgb)
  -- A chevron pointing left.
  local t = s * 0.22
  shape(string.format('m %d %d l %d %d l %d %d l %d %d l %d %d l %d %d',
    x + s * 0.7, y, x + s * 0.7 + t, y + t, x + s * 0.3 + t, y + s / 2, x + s * 0.7 + t, y + s - t, x + s * 0.7, y + s, x + s * 0.3 - t * 0.3, y + s / 2), rgb)
end

local function icon_menu(x, y, s, rgb)
  local h = math.floor(s * 0.14)
  for i = 0, 2 do
    local yy = y + math.floor(i * (s - h) / 2)
    box(x, yy, x + s, yy + h, h / 2, rgb)
  end
end

-- A key hint like the remote's buttons: [OK] Skip intro
local function key_chip(x, y, label, size)
  local w = math.max(size * 1.6, #label * size * 0.62 + size * 0.7)
  box(x, y - size * 0.62, x + w, y + size * 0.62, size * 0.3, 'ffffff', 0xC8)
  text(x + w / 2, y, 5, size * 0.78, label, 'ffffff', 0, true)
  return w
end

-- Behaviour helpers ---------------------------------------------------------

local function live() return state.mode == 'live' end

-- How far playback is behind the live edge, in seconds.
local function behind_live()
  local c, t = prop.cache, prop['time-pos']
  if not c or not t or not c['cache-end'] then return 0 end
  return math.max(0, c['cache-end'] - t)
end

local function controls_visible()
  if menu then return false end
  if prop.pause or prop['paused-for-cache'] then return true end
  return mp.get_time() < shown_until
end

function poke(keep)
  shown_until = mp.get_time() + HIDE_AFTER
  if keep ~= false or controls_visible() then render() end
end

local function say(s)
  toast.text = s
  toast.till = mp.get_time() + 3.5
  render()
end

local function send(...) mp.commandv('script-message', ...) end

local function seek(by)
  if live() and by > 0 and behind_live() < by + 2 then
    -- Past the live edge: just go live.
    local c = prop.cache
    if c and c['cache-end'] then mp.commandv('seek', tostring(c['cache-end'] - 1), 'absolute') end
  else
    mp.commandv('seek', tostring(by), 'relative')
  end
  poke()
end

local function go_live()
  local c = prop.cache
  if c and c['cache-end'] then mp.commandv('seek', tostring(c['cache-end'] - 1), 'absolute') end
  mp.set_property_native('pause', false)
  poke()
end

local function toggle_pause()
  mp.set_property_native('pause', not prop.pause)
  poke()
end

-- The menu ---------------------------------------------------------------

local function track_label(t)
  local parts = {}
  if t.title and t.title ~= '' then parts[#parts + 1] = t.title end
  if t.lang and t.lang ~= '' and not (t.title or ''):lower():find(t.lang:lower(), 1, true) then
    parts[#parts + 1] = t.lang:upper()
  end
  if t.type == 'audio' then
    local ch = t['demux-channel-count']
    local codec = (t.codec or ''):upper():gsub('EAC3', 'E-AC3'):gsub('TRUEHD', 'TrueHD')
    local extra = codec
    if ch then extra = extra .. (ch == 6 and ' 5.1' or ch == 8 and ' 7.1' or ch == 2 and ' Stereo' or ch == 1 and ' Mono' or ' ' .. ch .. 'ch') end
    if extra ~= '' then parts[#parts + 1] = extra end
  elseif t.external then
    parts[#parts + 1] = 'File'
  end
  if t.forced then parts[#parts + 1] = 'Forced' end
  if #parts == 0 then parts[1] = 'Track ' .. t.id end
  return table.concat(parts, ' · ')
end

local function build_rows()
  local rows = {}
  local function section(title) rows[#rows + 1] = { header = title } end
  for _, s in ipairs(page_menu) do
    if s.items and #s.items > 0 then
      section(s.title or '')
      for _, it in ipairs(s.items) do
        rows[#rows + 1] = { id = tostring(it.id or ''), label = it.label or '', active = it.active, disabled = it.disabled or not it.id }
      end
    end
  end
  local audio, subs = {}, {}
  for _, t in ipairs(prop['track-list'] or {}) do
    if t.type == 'audio' then audio[#audio + 1] = t elseif t.type == 'sub' then subs[#subs + 1] = t end
  end
  if #audio > 1 then
    section('Audio')
    for _, t in ipairs(audio) do rows[#rows + 1] = { id = 'aid:' .. t.id, label = track_label(t), active = t.selected } end
  end
  if #subs > 0 then
    section(live() and 'Captions' or 'Subtitles')
    rows[#rows + 1] = { id = 'sid:no', label = 'Off', active = prop.sid == false or prop.sid == 'no' }
    for _, t in ipairs(subs) do rows[#rows + 1] = { id = 'sid:' .. t.id, label = track_label(t), active = t.selected } end
  end
  return rows
end

local function selectable(r) return r and not r.header and not r.disabled end

local close_menu

local function focus_step(dir)
  if not menu then return end
  local i = menu.focus
  repeat
    i = i + dir
  until i < 1 or i > #menu.rows or selectable(menu.rows[i])
  if i >= 1 and i <= #menu.rows then menu.focus = i end
  render()
end

local function pick(row)
  if not row or not selectable(row) then return end
  local kind, val = row.id:match('^(%a+):(.*)$')
  if kind == 'aid' or kind == 'sid' then
    mp.set_property(kind, val)
    -- Rebuild for the new check marks; keep the focus where it is.
    local f = menu.focus
    menu.rows = build_rows()
    menu.focus = math.min(f, #menu.rows)
    render()
  else
    close_menu()
    send('bb-pick', row.id)
  end
end

local menu_keys = {
  { 'UP', function() focus_step(-1) end, true },
  { 'DOWN', function() focus_step(1) end, true },
  { 'PGUP', function() for _ = 1, 8 do focus_step(-1) end end, true },
  { 'PGDWN', function() for _ = 1, 8 do focus_step(1) end end, true },
  { 'WHEEL_UP', function() focus_step(-1) end },
  { 'WHEEL_DOWN', function() focus_step(1) end },
  { 'HOME', function() menu.focus = 0; focus_step(1) end },
  { 'END', function() menu.focus = #menu.rows + 1; focus_step(-1) end },
  { 'ENTER', function() pick(menu.rows[menu.focus]) end },
  { 'KP_ENTER', function() pick(menu.rows[menu.focus]) end },
  { 'RIGHT', function() pick(menu.rows[menu.focus]) end },
  { 'LEFT', function() close_menu() end },
  { 'ESC', function() close_menu() end },
  { 'BS', function() close_menu() end },
  { 'MBTN_BACK', function() close_menu() end },
  { 'MENU', function() close_menu() end },
  { 'TAB', function() close_menu() end },
  { 'm', function() close_menu() end },
}

local function open_menu()
  local rows = build_rows()
  if #rows == 0 then
    say('No options for this video')
    return
  end
  menu = { rows = rows, focus = 0, scroll = 0 }
  -- Start on the chosen entry of the first list (the channel playing now).
  for i, r in ipairs(rows) do
    if selectable(r) and r.active then menu.focus = i break end
  end
  if menu.focus == 0 then focus_step(1) end
  for _, k in ipairs(menu_keys) do
    mp.add_forced_key_binding(k[1], 'bb-menu-' .. k[1], k[2], k[3] and { repeatable = true } or nil)
  end
  render()
end

function close_menu()
  if not menu then return end
  menu = nil
  for _, k in ipairs(menu_keys) do mp.remove_key_binding('bb-menu-' .. k[1]) end
  poke()
end

local function toggle_menu()
  if menu then close_menu() else open_menu() end
end

-- Rendering --------------------------------------------------------------------

local function draw_top()
  shade(0, 230, true)
  local back_s = 44
  icon_back(72, 70, back_s, 'ffffff')
  hit(50, 50, 140, 140, function() mp.command('quit') end)
  local title = state.title ~= '' and state.title or prop['media-title'] or ''
  text(140, 92, 4, 50, clip_chars(title, math.floor((W - 640) / 27)), 'ffffff', 0, true)
  if state.subtitle ~= '' then
    text(140, 146, 4, 32, clip_chars(state.subtitle, math.floor((W - 640) / 16)), 'd8dee6', 0)
  end
  text(W - 80, 92, 6, 40, os.date('%H:%M'), 'ffffff', 0, true)
  if state.badge ~= '' then
    local w = #state.badge * 13 + 56
    box(W - 80 - w, 128, W - 80, 172, 22, ACCENT, 0x30)
    text(W - 80 - w / 2, 150, 5, 26, state.badge, 'ffffff', 0, true)
  end
  -- Menu button, next to the clock.
  icon_menu(W - 230, 74, 36, 'ffffff')
  hit(W - 250, 50, W - 180, 120, toggle_menu)
end

local function draw_bottom()
  shade(H - 300, H, false)
  local x0, x1, y = 96, W - 96, H - 128
  local t = prop['time-pos'] or 0
  local d, lo = prop.duration or 0, 0
  if live() then
    -- The bar is the part of the stream that can be rewound.
    local c = prop.cache
    local ranges = c and c['seekable-ranges']
    if ranges and ranges[1] then lo, d = ranges[1].start, ranges[#ranges]['end'] else lo, d = t, t end
    if c and c['cache-end'] then d = math.max(d, c['cache-end']) end
  end
  local span = math.max(d - lo, 0.001)
  local function xat(v) return x0 + (x1 - x0) * math.max(0, math.min(1, (v - lo) / span)) end

  -- Track, buffered, markers, played, knob.
  box(x0, y - 5, x1, y + 5, 5, 'ffffff', 0xB0)
  local c = prop.cache
  if not live() and c and c['cache-end'] and d > 0 then box(x0, y - 5, xat(c['cache-end']), y + 5, 5, 'ffffff', 0x80) end
  if not live() and d > 0 then
    for _, m in ipairs(state.markers or {}) do
      if m['end'] and m.start and m['end'] > m.start then box(xat(m.start), y - 5, xat(m['end']), y + 5, 0, 'fbbf24', 0x40) end
    end
  end
  if d > lo then box(x0, y - 5, xat(t), y + 5, 5, ACCENT, 0) end
  local kx = xat(t)
  box(kx - 13, y - 13, kx + 13, y + 13, 13, 'ffffff', 0)
  hit(x0, y - 30, x1, y + 30, function(mx)
    if d <= lo then return end
    local v = lo + span * (mx - x0) / (x1 - x0)
    mp.commandv('seek', tostring(v), 'absolute')
    poke()
  end)

  -- Below the bar: play/pause, the time, and the end or the live badge.
  local iy = H - 88
  if prop.pause then icon_play(x0 + 4, iy, 40, 'ffffff') else icon_pause(x0, iy, 40, 'ffffff') end
  hit(x0 - 20, iy - 20, x0 + 60, iy + 60, toggle_pause)
  if live() then
    local b = behind_live()
    if b >= 5 then
      local label = '−' .. fmt_time(b) .. '   GO LIVE'
      local w = #label * 17 + 40
      box(x1 - w, iy - 2, x1, iy + 46, 24, 'ffffff', 0x40)
      text(x1 - w / 2, iy + 22, 5, 28, label, 'ffffff', 0, true)
      hit(x1 - w, iy - 10, x1, iy + 54, go_live)
    else
      box(x1 - 120, iy - 2, x1, iy + 46, 24, 'e11d48', 0)
      text(x1 - 60, iy + 22, 5, 28, 'LIVE', 'ffffff', 0, true)
    end
    text(x0 + 76, iy + 20, 4, 34, os.date('%H:%M'), 'ffffff', 0, true)
  else
    text(x0 + 76, iy + 20, 4, 34, fmt_time(t) .. '  /  ' .. fmt_time(d), 'ffffff', 0, true)
    if d > 0 then
      local ends = os.date('%H:%M', os.time() + math.floor(d - t))
      text(x1, iy + 20, 6, 30, '−' .. fmt_time(d - t) .. '   ·   ends at ' .. ends, 'd8dee6', 0)
    end
  end
  -- What the keys do, quietly in the middle.
  local hint = live() and '←  10 s      30 s  →      ↑ ↓  channels      OK  pause'
    or '←  10 s      30 s  →      OK  pause'
  text(W / 2, iy + 20, 5, 26, hint, 'ffffff', 0x60)
end

-- Skip intro / credits, and Up next: shown even while the controls are hidden.
local function draw_prompt()
  local y = (controls_visible() and H - 240) or H - 130
  if state.upNext ~= '' then
    local w, h = 640, 184
    local x0 = W - 96 - w
    box(x0, y - h, x0 + w, y, 18, '101418', 0x18)
    text(x0 + 36, y - h + 42, 4, 26, 'UP NEXT', ACCENT, 0, true)
    text(x0 + 36, y - h + 88, 4, 38, clip_chars(state.upNext, 30), 'ffffff', 0, true)
    local cx = x0 + 36
    cx = cx + key_chip(cx, y - 40, 'OK', 30) + 14
    text(cx, y - 40, 4, 28, 'Play now', 'ffffff', 0)
    cx = cx + 140
    cx = cx + key_chip(cx, y - 40, 'Back', 30) + 14
    text(cx, y - 40, 4, 28, 'Stop', 'ffffff', 0)
    hit(x0, y - h, x0 + w, y, function() send('bb-enter') end)
  elseif state.prompt ~= '' then
    local label = state.prompt
    local w = #label * 19 + 150
    local x0 = W - 96 - w
    box(x0, y - 76, x0 + w, y, 38, ACCENT, 0)
    text(x0 + 40, y - 38, 4, 34, label, INK, 0, true)
    local cw = 64
    box(x0 + w - cw - 24, y - 58, x0 + w - 24, y - 18, 12, INK, 0)
    text(x0 + w - 24 - cw / 2, y - 38, 5, 24, 'OK', ACCENT, 0, true)
    hit(x0, y - 76, x0 + w, y, function() send('bb-enter') end)
  end
end

local function draw_menu()
  local pw = 680
  local x0 = W - pw
  box(x0, 0, W, H, 0, '0b0d10', 0x18)
  box(x0, 0, x0 + 2, H, 0, 'ffffff', 0xD0)
  text(x0 + 48, 70, 4, 40, 'Options', 'ffffff', 0, true)
  -- Rows, scrolled to keep the focused one in view.
  local top, bottom = 120, H - 70
  local ys, y = {}, 0
  for i, r in ipairs(menu.rows) do
    if r.header then y = y + (i > 1 and 24 or 0) end
    ys[i] = y
    y = y + (r.header and 52 or 64)
  end
  local f = menu.focus
  if f >= 1 then
    local fy, fh = ys[f], 64
    if fy - menu.scroll < 0 then menu.scroll = math.max(0, fy - 60) end
    if fy + fh - menu.scroll > bottom - top then menu.scroll = fy + fh - (bottom - top) + 60 end
    -- Keep a header above the first entry visible.
    if f >= 1 and menu.scroll > 0 then
      local first = true
      for i = 1, f - 1 do if selectable(menu.rows[i]) then first = false end end
      if first then menu.scroll = 0 end
    end
  end
  local clip = string.format('\\clip(%d,%d,%d,%d)', x0, top, W, bottom)
  for i, r in ipairs(menu.rows) do
    local ry = top + ys[i] - menu.scroll
    if ry + 64 >= top and ry <= bottom then
      if r.header then
        text(x0 + 48, ry + 34, 4, 24, r.header:upper(), ACCENT, 0, true, clip)
      else
        local focused = i == f
        if focused then box(x0 + 28, ry + 4, W - 28, ry + 60, 14, ACCENT, 0, clip) end
        local rgb = focused and INK or (r.disabled and '9aa4b0' or 'ffffff')
        if r.active then box(x0 + 52, ry + 25, x0 + 66, ry + 39, 7, focused and INK or ACCENT, 0, clip) end
        text(x0 + 84, ry + 32, 4, 32, clip_chars(r.label, 34), rgb, 0, focused or r.active, clip)
        if selectable(r) then
          hit(x0, math.max(ry, top), W, math.min(ry + 64, bottom), function()
            menu.focus = i
            pick(r)
          end)
        end
      end
    end
  end
  -- Arrows when there's more above or below.
  if menu.scroll > 0 then
    shape(string.format('m %d %d l %d %d l %d %d', W - 72, top - 8, W - 48, top - 8, W - 60, top - 22), 'ffffff', 0x60)
  end
  if ys[#menu.rows] and ys[#menu.rows] + 64 - menu.scroll > bottom - top then
    shape(string.format('m %d %d l %d %d l %d %d', W - 72, bottom + 10, W - 48, bottom + 10, W - 60, bottom + 24), 'ffffff', 0x60)
  end
  hit(0, 0, x0, H, close_menu)
end

local function draw_toast()
  local s = toast.text
  local w = math.min(W - 200, #s * 17 + 80)
  local y = controls_visible() and 210 or 70
  box(W / 2 - w / 2, y, W / 2 + w / 2, y + 68, 34, '101418', 0x30)
  text(W / 2, y + 34, 5, 30, clip_chars(s, math.floor((w - 60) / 16)), 'ffffff', 0)
end

local function draw_loading()
  local s = prop['paused-for-cache'] and 'Buffering…' or 'Loading…'
  box(W / 2 - 140, H / 2 - 40, W / 2 + 140, H / 2 + 40, 40, '101418', 0x40)
  text(W / 2, H / 2, 5, 32, s, 'ffffff', 0)
end

function render()
  local osd = prop.osd
  if not osd or not osd.w or osd.w == 0 or osd.h == 0 then return end
  H = 1080
  W = math.floor(H * osd.w / osd.h)
  overlay.res_x, overlay.res_y = W, H
  out, hits = {}, {}
  local now = mp.get_time()
  local visible = controls_visible()
  if visible then
    draw_top()
    draw_bottom()
  end
  if not menu and (state.prompt ~= '' or state.upNext ~= '') then draw_prompt() end
  local waiting = prop['paused-for-cache'] or (prop['time-pos'] == nil and not prop['idle-active'])
  if waiting and not menu then draw_loading() end
  if menu then draw_menu() end
  if now < toast.till then draw_toast() end
  overlay.data = table.concat(out, '\n')
  overlay:update()
  -- Redraw while anything moves or is about to fade; idle otherwise.
  local busy = visible or menu or now < toast.till or waiting
  if busy and not timer:is_enabled() then timer:resume() elseif not busy and timer:is_enabled() then timer:kill() end
end

timer = mp.add_periodic_timer(0.25, function() render() end)
timer:kill()

-- Input --------------------------------------------------------------------------

local function mouse_xy()
  local m = mp.get_property_native('mouse-pos')
  local osd = prop.osd
  if not m or not osd or osd.w == 0 then return nil end
  return m.x * W / osd.w, m.y * H / osd.h
end

-- Clicks: on a control, it acts; elsewhere, show or hide the controls.
mp.add_forced_key_binding('MBTN_LEFT', 'bb-click', function()
  local x, y = mouse_xy()
  if x then
    for i = #hits, 1, -1 do
      local h = hits[i]
      if x >= h[1] and x <= h[3] and y >= h[2] and y <= h[4] then
        h[5](x, y)
        return
      end
    end
  end
  if menu then return end
  if controls_visible() then shown_until = 0 render() else poke() end
end)

mp.observe_property('mouse-pos', 'native', function(_, m)
  if m and m.hover then poke(false) end
end)

local function bind(name, key, fn, repeatable)
  mp.add_key_binding(key, name, fn, repeatable and { repeatable = true } or nil)
end

local function back()
  if menu then close_menu() else mp.command('quit') end
end
bind('back', 'ESC', back)
bind('back-bs', 'BS', back)
bind('back-remote', 'MBTN_BACK', back)
local function ok()
  if state.prompt ~= '' or state.upNext ~= '' or not live() then
    -- The app decides: skip, play the next episode, or pause.
    send('bb-enter')
    poke(false)
  elseif not controls_visible() then
    poke()
  else
    toggle_pause()
  end
end
bind('ok', 'ENTER', ok)
bind('ok-kp', 'KP_ENTER', ok)
bind('left', 'LEFT', function() seek(-10) end, true)
bind('right', 'RIGHT', function() seek(30) end, true)
bind('up', 'UP', function()
  if live() then send('bb-channel', 'up') else
    mp.commandv('add', 'volume', '5')
    say('Volume ' .. math.floor(mp.get_property_number('volume', 0) + 0.5) .. '%')
  end
end, true)
bind('down', 'DOWN', function()
  if live() then send('bb-channel', 'down') else
    mp.commandv('add', 'volume', '-5')
    say('Volume ' .. math.floor(mp.get_property_number('volume', 0) + 0.5) .. '%')
  end
end, true)
bind('channel-up', 'PGUP', function() if live() then send('bb-channel', 'up') end end)
bind('channel-down', 'PGDWN', function() if live() then send('bb-channel', 'down') end end)
bind('next', 'n', function() send('bb-next') end)
bind('menu', 'TAB', toggle_menu)
bind('menu-m', 'm', toggle_menu)
bind('menu-remote', 'MENU', toggle_menu)
bind('go-live', 'l', function() if live() then go_live() end end)
bind('play-pause', 'SPACE', toggle_pause)

-- Messages from the app ---------------------------------------------------------------

mp.register_script_message('bb-state', function(json)
  local s = utils.parse_json(json or '')
  if type(s) ~= 'table' then return end
  local had_prompt = state.prompt ~= '' or state.upNext ~= ''
  state = {
    mode = s.mode or 'vod',
    title = s.title or '',
    subtitle = s.subtitle or '',
    markers = type(s.markers) == 'table' and s.markers or {},
    prompt = s.prompt or '',
    upNext = s.upNext or '',
    badge = s.badge or '',
  }
  if not had_prompt and (state.prompt ~= '' or state.upNext ~= '') then poke(false) end
  render()
end)

mp.register_script_message('bb-menu', function(json)
  local m = utils.parse_json(json or '')
  page_menu = type(m) == 'table' and m or {}
  if menu then
    local id = menu.rows[menu.focus] and menu.rows[menu.focus].id
    menu.rows = build_rows()
    menu.focus = 0
    for i, r in ipairs(menu.rows) do if r.id == id then menu.focus = i end end
    if menu.focus == 0 then focus_step(1) end
    render()
  end
end)

mp.register_script_message('bb-toast', function(s) say(s or '') end)
mp.register_script_message('bb-menu-open', function() if not menu then open_menu() end end)

-- A new file (the next episode, another channel) starts with the controls up.
mp.register_event('file-loaded', function() poke() end)
mp.register_event('seek', function() poke() end)

-- The app sends its state again once it hears this (it may have tried
-- before this script had started).
send('bb-ui-ready')
