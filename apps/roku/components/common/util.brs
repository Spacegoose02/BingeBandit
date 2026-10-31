' Helpers shared by every component (each component's XML includes this file).

' --- Session -----------------------------------------------------------------

' The signed-in server and token live in the registry so the channel opens
' straight to Home next time.
function loadSession() as boolean
    sec = CreateObject("roRegistrySection", "bingebandit")
    if not sec.Exists("session") then return false
    s = ParseJson(sec.Read("session"))
    if s = invalid or s.server = invalid or s.token = invalid or s.token = "" then return false
    applySession(s)
    return true
end function

sub saveSession(s as dynamic)
    applySession(s)
    sec = CreateObject("roRegistrySection", "bingebandit")
    if s = invalid
        sec.Delete("session")
    else
        sec.Write("session", FormatJson(s))
    end if
    sec.Flush()
end sub

sub applySession(s as dynamic)
    if s = invalid
        m.global.token = ""
        m.global.user = {}
        m.global.profileChosen = false
        m.global.alternates = []
        m.global.serverId = ""
        return
    end if
    m.global.server = s.server
    ' The same server's other addresses (LAN, public URL), tried when this
    ' one stops answering; the id makes sure an address is this server.
    if type(s.alternates) = "roArray" then m.global.alternates = s.alternates else m.global.alternates = []
    m.global.serverId = asString(s.serverId)
    m.global.token = s.token
    if s.user <> invalid then m.global.user = s.user else m.global.user = {}
    m.global.profileChosen = isTrue(s.profileChosen)
end sub

' The last server, kept after signing out so the next sign-in can skip finding it.
function lastServer() as dynamic
    sec = CreateObject("roRegistrySection", "bingebandit")
    if not sec.Exists("server") then return invalid
    return ParseJson(sec.Read("server"))
end function

sub rememberServer(info as object)
    sec = CreateObject("roRegistrySection", "bingebandit")
    sec.Write("server", FormatJson(info))
    sec.Flush()
end sub

function currentSession() as object
    return { server: m.global.server, alternates: m.global.alternates, serverId: m.global.serverId, token: m.global.token, user: m.global.user, profileChosen: m.global.profileChosen }
end function

' --- URLs --------------------------------------------------------------------

' Adds the session token to a server path, for Posters and the Video node,
' which load outside roUrlTransfer. The server takes ?token= on GETs.
function withToken(url as string) as string
    if m.global.token = "" then return url
    if Instr(1, url, "?") > 0 then sep = "&" else sep = "?"
    return url + sep + "token=" + m.global.token
end function

function absoluteUrl(path as string) as string
    if Left(path, 1) = "/" then return m.global.server + path
    return path
end function

' Artwork URL at a TMDB width (w185 w342 w500 w780 w1280).
function imageUrl(url as dynamic, width as string) as string
    if url = invalid or url = "" then return ""
    full = absoluteUrl(url)
    if Instr(1, full, "/api/images/") > 0
        if Instr(1, full, "?") > 0 then full = full + "&w=" + width else full = full + "?w=" + width
    end if
    if Instr(1, full, m.global.server + "/api/") = 1 then full = withToken(full)
    return full
end function

function authHeaders() as object
    if m.global.token = "" then return []
    return ["Authorization:Bearer " + m.global.token]
end function

' --- Formatting --------------------------------------------------------------

function clockTime(t as dynamic) as string
    if t = invalid or t < 0 then t = 0
    t = Int(t)
    h = Int(t / 3600)
    mm = Int((t mod 3600) / 60)
    s = t mod 60
    out = ""
    if h > 0 then out = h.ToStr() + ":" + pad2(mm) else out = mm.ToStr()
    return out + ":" + pad2(s)
end function

function pad2(n as integer) as string
    if n < 10 then return "0" + n.ToStr()
    return n.ToStr()
end function

' "8:30 PM" for a Unix time, in the Roku's time zone.
function wallClock(unix as dynamic) as string
    if unix = invalid then return ""
    d = CreateObject("roDateTime")
    d.FromSeconds(unix)
    d.ToLocalTime()
    h = d.GetHours()
    ampm = "AM"
    if h >= 12 then ampm = "PM"
    h = h mod 12
    if h = 0 then h = 12
    return h.ToStr() + ":" + pad2(d.GetMinutes()) + " " + ampm
end function

function runtimeLabel(minutes as dynamic) as string
    if minutes = invalid or minutes <= 0 then return ""
    h = Int(minutes / 60)
    mm = minutes mod 60
    if h = 0 then return mm.ToStr() + "m"
    if mm = 0 then return h.ToStr() + "h"
    return h.ToStr() + "h " + mm.ToStr() + "m"
end function

function episodeCode(it as object) as string
    s = ""
    if it.seasonNumber <> invalid then s = "S" + it.seasonNumber.ToStr()
    if it.episodeNumber <> invalid
        if s <> "" then s = s + " "
        s = s + "E" + it.episodeNumber.ToStr()
    end if
    return s
end function

function joinStrings(parts as object, sep as string) as string
    out = ""
    for each p in parts
        if p <> invalid and p <> ""
            if out <> "" then out = out + sep
            out = out + p
        end if
    end for
    return out
end function

' The first of these keys with a non-empty value.
function firstOf(aa as object, keys as object) as dynamic
    for each k in keys
        v = aa[k]
        if v <> invalid
            if type(v) <> "roString" and type(v) <> "String" then return v
            if v <> "" then return v
        end if
    end for
    return invalid
end function

function getOr(aa as dynamic, key as string, fallback as dynamic) as dynamic
    if aa = invalid then return fallback
    v = aa[key]
    if v = invalid then return fallback
    return v
end function

function asString(v as dynamic) as string
    if v = invalid then return ""
    t = type(v)
    if t = "roString" or t = "String" then return v
    if t = "roInt" or t = "roInteger" or t = "Integer" or t = "roLongInteger" or t = "LongInteger" then return v.ToStr()
    if t = "roFloat" or t = "Float" or t = "roDouble" or t = "Double" then return Str(v).Trim()
    if t = "roBoolean" or t = "Boolean"
        if v then return "true" else return "false"
    end if
    return ""
end function

' --- Requests from the render thread -----------------------------------------

' Runs a request on an ApiTask and calls `callback` (a function name in the
' calling component) with the task's response field. The task is kept until it
' answers so it isn't collected mid-flight.
function api(method as string, path as string, body as dynamic, callback as string, extra = invalid as dynamic) as object
    req = { method: method, path: path }
    if body <> invalid then req.body = body
    if extra <> invalid then req.Append(extra)
    t = CreateObject("roSGNode", "ApiTask")
    t.request = req
    t.observeField("response", callback)
    if m.pendingTasks = invalid then m.pendingTasks = []
    m.pendingTasks.Push(t)
    t.control = "run"
    return t
end function

' Drops a finished task from the pending list (call from the callback).
sub doneTask(t as object)
    if m.pendingTasks = invalid then return
    for i = m.pendingTasks.Count() - 1 to 0 step -1
        if m.pendingTasks[i].isSameNode(t) then m.pendingTasks.Delete(i)
    end for
end sub

' Fire-and-forget request (progress reports, leaving a channel).
sub apiSend(method as string, path as string, body as dynamic)
    api(method, path, body, "onFireAndForget")
end sub

' For a screen that's closing (the player's last progress report): the task
' is parked on the scene so it outlives the screen, and removes itself.
sub apiSendDetached(method as string, path as string, body as dynamic)
    req = { method: method, path: path, detached: true }
    if body <> invalid then req.body = body
    t = CreateObject("roSGNode", "ApiTask")
    t.request = req
    m.top.getScene().appendChild(t)
    t.control = "run"
end sub

sub onFireAndForget(event as object)
    doneTask(event.getRoSGNode())
end sub

' Where selecting a tile goes: a collection opens as a grid, anything else
' its details page.
function openItem(c as object) as object
    if c.kind = "collection"
        return { screen: "GridScreen", params: { title: c.title, path: "/api/collections/" + c.itemId.ToStr(), build: "items" } }
    end if
    return { screen: "DetailsScreen", params: { id: c.itemId } }
end function

' True only for a real boolean true; missing JSON fields are invalid, and
' comparing invalid with = is a type mismatch on some firmware.
function isTrue(v as dynamic) as boolean
    if type(v) = "roBoolean" or type(v) = "Boolean" then return v
    return false
end function
