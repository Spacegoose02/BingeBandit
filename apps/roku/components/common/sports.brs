' Helpers for the Sports Hub (SportsView, GameItem, GameScreen and ApiTask's
' "sports" build), a port of the TV app's src/sports/hub.ts.

function sportName(sport as dynamic) as string
    names = { football: "Football", basketball: "Basketball", baseball: "Baseball", hockey: "Hockey", soccer: "Soccer", mma: "UFC", racing: "NASCAR" }
    n = names[asString(sport)]
    if n = invalid then return asString(sport)
    return n
end function

' Each sport's stripe colour on game tiles.
function sportColor(sport as dynamic) as string
    colors = { football: "0xF5B942FF", basketball: "0xF59E42FF", baseball: "0xF87171FF", hockey: "0x60A5FAFF", soccer: "0x4ADE80FF", mma: "0xE5E7EBFF", racing: "0xFACC15FF" }
    c = colors[asString(sport)]
    if c = invalid then return "0x2DD4BFFF"
    return c
end function

' UFC and NASCAR have fighters and drivers, not teams to follow or box scores.
function hasTeams(sport as dynamic) as boolean
    s = asString(sport)
    return s <> "mma" and s <> "racing"
end function

' Seconds since 1970 for the server's RFC 3339 times ("2026-11-01T17:00:00Z").
function isoSeconds(iso as dynamic) as integer
    s = asString(iso)
    if s = "" then return 0
    d = CreateObject("roDateTime")
    d.FromISO8601String(s)
    return d.AsSeconds()
end function

' A day as the server takes it (YYYYMMDD), offset days from today here.
function dayKey(offset as integer) as string
    d = CreateObject("roDateTime")
    d.FromSeconds(d.AsSeconds() + offset * 86400)
    d.ToLocalTime()
    return d.GetYear().ToStr() + pad2(d.GetMonth()) + pad2(d.GetDayOfMonth())
end function

function dayLabel(offset as integer) as string
    if offset = 0 then return "Today"
    if offset = -1 then return "Yesterday"
    if offset = 1 then return "Tomorrow"
    d = CreateObject("roDateTime")
    d.FromSeconds(d.AsSeconds() + offset * 86400)
    d.ToLocalTime()
    return shortDate(d)
end function

' "Sat, Nov 1"
function shortDate(d as object) as string
    months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
    return Left(d.GetWeekday(), 3) + ", " + months[d.GetMonth() - 1] + " " + d.GetDayOfMonth().ToStr()
end function

' "Today 7:30 PM", "Tomorrow 1:00 PM" or "Sat, Nov 8 7:30 PM".
function startLabel(iso as dynamic) as string
    t = isoSeconds(iso)
    if t = 0 then return ""
    d = CreateObject("roDateTime")
    d.FromSeconds(t)
    d.ToLocalTime()
    today = CreateObject("roDateTime")
    today.ToLocalTime()
    tomorrow = CreateObject("roDateTime")
    tomorrow.FromSeconds(tomorrow.AsSeconds() + 86400)
    tomorrow.ToLocalTime()
    day = shortDate(d)
    if day = shortDate(today)
        day = "Today"
    else if day = shortDate(tomorrow)
        day = "Tomorrow"
    end if
    return day + " " + wallClock(t)
end function

' Whether a game's score shows: not before it starts, and not in spoiler-free
' mode until someone asks to see that game's (m.global.revealed, kept until
' the channel closes).
function scoreShown(g as object) as boolean
    if getOr(g, "state", "pre") = "pre" then return false
    if not isTrue(m.global.spoilerFree) then return true
    return isTrue(m.global.revealed[asString(g.id)])
end function

' The side's name to show: "Hokies", or the abbreviation when there's none.
function sideName(side as dynamic, long = false as boolean) as string
    if side = invalid then return ""
    if long
        n = getOr(side, "name", "")
        if n <> "" then return n
    end if
    n = getOr(side, "shortName", "")
    if n = "" then n = getOr(side, "abbr", "")
    if n = "" then n = getOr(side, "name", "")
    return n
end function

' A race has empty sides and its running order instead.
function isRace(g as object) as boolean
    race = getOr(g, "race", invalid)
    return type(race) = "roArray" or (getOr(g.home, "id", "") = "" and getOr(g.away, "id", "") = "")
end function

' "5 ESPN, 206 ESPN2": the game's Live TV channels.
function channelsText(g as object) as string
    parts = []
    for each c in getOr(g, "channels", [])
        parts.Push(joinStrings([asString(c.number), getOr(c, "name", "")], " "))
    end for
    return joinStrings(parts, ", ")
end function

' What the game is doing: "3rd Qtr 4:12", "Final", or when it starts.
function gameStatus(g as object) as string
    state = getOr(g, "state", "pre")
    if state = "pre" then return startLabel(g.start)
    return getOr(g, "detail", "")
end function
