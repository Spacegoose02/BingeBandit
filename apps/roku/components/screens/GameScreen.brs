' One game from the Sports Hub. The list on the left has what you can do
' (watch it on your channel, show the score in spoiler-free mode, follow or
' stop following its teams) and the parts of its summary: score by period,
' leaders, team stats, each team's player stats and the standings, drawn in
' the panel beside it as they're focused. A live game refreshes every 30 s.

sub init()
    m.list = m.top.findNode("list")
    m.panel = m.top.findNode("panel")
    m.note = m.top.findNode("note")
    m.list.observeField("itemFocused", "onFocused")
    m.list.observeField("itemSelected", "onChosen")
    m.top.observeField("params", "onParams")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.top.observeField("closing", "onClosing")
    m.reload = m.top.findNode("reload")
    m.reload.observeField("fire", "loadSummary")
    m.game = invalid
    m.sum = invalid
    m.sumError = ""
    m.teams = invalid ' the viewer's teams, loaded for Follow
    m.entries = []
    m.section = ""
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.list.setFocus(true)
end sub

sub onClosing()
    if m.top.closing then m.reload.control = "stop"
end sub

sub onParams()
    p = m.top.params
    m.game = p.game
    m.sport = getOr(p, "sport", "")
    m.leagueName = getOr(p, "leagueName", "")
    m.top.findNode("stripe").color = sportColor(m.sport)
    drawHeader()
    build()
    loadSummary()
    if hasTeams(m.sport) and not isRace(m.game) then api("GET", "/api/sports", invalid, "onTeams", { timeout: 30000 })
    if getOr(m.game, "state", "") = "in" then m.reload.control = "start"
end sub

sub loadSummary()
    g = m.game
    api("GET", "/api/sports/" + g.league + "/games/" + asString(g.id), invalid, "onSummary", { timeout: 30000 })
end sub

sub onSummary(event as object)
    doneTask(event.getRoSGNode())
    if m.top.closing then return
    res = event.getData()
    if not res.ok or type(res.data) <> "roAssociativeArray"
        m.sumError = asString(res.error)
        if m.sum = invalid then drawSection()
        return
    end if
    m.sum = res.data
    m.sumError = ""
    ' The summary's game is newer than the hub's: keep its channels.
    sg = getOr(m.sum, "game", invalid)
    if type(sg) = "roAssociativeArray" and getOr(sg, "id", "") <> ""
        sg.channels = getOr(m.game, "channels", [])
        sg.mine = getOr(m.game, "mine", [])
        if getOr(sg, "league", "") = "" then sg.league = m.game.league
        m.game = sg
    end if
    if getOr(m.game, "state", "") <> "in" then m.reload.control = "stop"
    drawHeader()
    build()
end sub

sub onTeams(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok and type(res.data) = "roAssociativeArray" then m.teams = getOr(res.data, "teams", [])
    build()
end sub

' --- Header -----------------------------------------------------------------------

sub drawHeader()
    g = m.game
    shown = scoreShown(g)
    m.top.findNode("kicker").text = m.leagueName
    state = getOr(g, "state", "pre")
    if isRace(g)
        m.top.findNode("awayName").text = getOr(g, "name", getOr(g, "shortName", ""))
        m.top.findNode("homeName").text = ""
        m.top.findNode("awayScore").text = ""
        m.top.findNode("homeScore").text = ""
        m.top.findNode("awayLogo").uri = ""
        m.top.findNode("homeLogo").uri = ""
    else
        m.top.findNode("awayName").text = joinStrings([sideName(g.away, true), recordOf(g.away)], "   ")
        m.top.findNode("homeName").text = joinStrings([sideName(g.home, true), recordOf(g.home)], "   ")
        m.top.findNode("awayLogo").uri = getOr(g.away, "logo", "")
        m.top.findNode("homeLogo").uri = getOr(g.home, "logo", "")
        a = ""
        h = ""
        if shown
            a = getOr(g.away, "score", "")
            h = getOr(g.home, "score", "")
        else if state <> "pre"
            a = "-"
            h = "-"
        end if
        m.top.findNode("awayScore").text = a
        m.top.findNode("homeScore").text = h
        muted = "0x98A1ADFF"
        white = "0xF2F4F7FF"
        m.top.findNode("awayScore").color = white
        m.top.findNode("homeScore").color = white
        if shown and state = "post"
            if isTrue(g.home.winner) then m.top.findNode("awayScore").color = muted
            if isTrue(g.away.winner) then m.top.findNode("homeScore").color = muted
        end if
    end if
    status = gameStatus(g)
    if state <> "pre" and not shown
        status = "Score hidden"
        if state = "in" then status = "Live  ·  score hidden"
    else if state = "in"
        status = "Live  ·  " + status
    end if
    tv = ""
    if getOr(g, "broadcasts", []).Count() > 0 then tv = "TV: " + joinStrings(g.broadcasts, ", ")
    m.top.findNode("meta").text = joinStrings([status, getOr(g, "venue", ""), tv], "  ·  ")
end sub

function recordOf(side as object) as string
    r = getOr(side, "record", "")
    if r = "" then return ""
    return "(" + r + ")"
end function

' --- The list: actions, then the summary's parts -------------------------------------

function isFollowing(team as object) as boolean
    if m.teams = invalid then return false
    for each t in m.teams
        if t.league = m.game.league and t.id = team.id then return true
    end for
    return false
end function

sub build()
    g = m.game
    entries = []
    for each ch in getOr(g, "channels", [])
        entries.Push({ label: "Watch on " + joinStrings([asString(ch.number), getOr(ch, "name", "")], " "), act: "watch", channel: ch })
    end for
    if getOr(g, "state", "pre") <> "pre" and not scoreShown(g) then entries.Push({ label: "Show the score", act: "reveal" })
    if m.teams <> invalid and not isRace(g)
        for each side in [g.away, g.home]
            if getOr(side, "id", "") <> ""
                if isFollowing(side) then label = "Stop following " + sideName(side) else label = "Follow " + sideName(side)
                entries.Push({ label: label, act: "follow", team: side })
            end if
        end for
    end if
    for each s in sections()
        entries.Push(s)
    end for
    ' Keep the focus on the same entry when the list is rebuilt.
    ' Still on the first one: stay on the first, as entries arrive above it.
    old = invalid
    if m.entries.Count() > 0 and m.list.itemFocused > 0 and m.list.itemFocused < m.entries.Count() then old = m.entries[m.list.itemFocused].label
    m.entries = entries
    root = CreateObject("roSGNode", "ContentNode")
    focus = 0
    for i = 0 to entries.Count() - 1
        root.createChild("ContentNode").title = entries[i].label
        if old <> invalid and entries[i].label = old then focus = i
    end for
    m.list.content = root
    m.list.jumpToItem = focus
    onFocused()
end sub

' The parts of the summary there's something to show for.
function sections() as object
    out = []
    g = m.game
    if isRace(g)
        if getOr(g, "race", []).Count() > 0 then out.Push({ label: "Running order", act: "section", section: "race" })
    else if getOr(g.away, "lines", []).Count() > 0 or getOr(g.home, "lines", []).Count() > 0
        out.Push({ label: "Score by period", act: "section", section: "lines" })
    end if
    s = m.sum
    if s = invalid
        if out.Count() = 0 then out.Push({ label: "Summary", act: "section", section: "none" })
        return out
    end if
    if getOr(s, "leaders", []).Count() > 0 then out.Push({ label: "Leaders", act: "section", section: "leaders" })
    if getOr(s, "stats", []).Count() > 0 then out.Push({ label: "Team stats", act: "section", section: "stats" })
    players = getOr(s, "players", [])
    for i = 0 to players.Count() - 1
        t = players[i]
        team = ""
        if getOr(g.away, "id", "") = t.team then team = sideName(g.away)
        if getOr(g.home, "id", "") = t.team then team = sideName(g.home)
        out.Push({ label: joinStrings([team, titleCase(getOr(t, "title", ""))], ": "), act: "section", section: "players", index: i })
    end for
    st = getOr(s, "standings", [])
    for i = 0 to st.Count() - 1
        out.Push({ label: joinStrings(["Standings", getOr(st[i], "title", "")], ": "), act: "section", section: "standings", index: i })
    end for
    if out.Count() = 0 then out.Push({ label: "Summary", act: "section", section: "none" })
    return out
end function

function titleCase(s as string) as string
    if s = "" then return s
    return UCase(Left(s, 1)) + Mid(s, 2)
end function

sub onFocused()
    e = m.entries[m.list.itemFocused]
    if e = invalid then return
    ' An action shows the first part of the summary beside it.
    if e.act <> "section"
        for each x in m.entries
            if x.act = "section"
                e = x
                exit for
            end if
        end for
    end if
    m.focusedSection = e
    drawSection()
end sub

sub onChosen()
    e = m.entries[m.list.itemSelected]
    if e = invalid then return
    if e.act = "watch"
        m.top.navigate = { screen: "LivePlayerScreen", params: { channels: [e.channel], index: 0 } }
    else if e.act = "reveal"
        r = m.global.revealed
        r[asString(m.game.id)] = true
        m.global.revealed = r
        drawHeader()
        build()
    else if e.act = "follow"
        follow(e.team)
    end if
end sub

sub follow(team as object)
    on = not isFollowing(team)
    list = []
    for each t in m.teams
        if not (t.league = m.game.league and t.id = team.id) then list.Push({ league: t.league, id: t.id })
    end for
    if on then list.Push({ league: m.game.league, id: team.id })
    api("PUT", "/api/sports/teams", list, "onFollowed", { name: sideName(team, true), on: on, timeout: 30000 })
end sub

sub onFollowed(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        m.note.text = res.error
        return
    end if
    if type(res.data) = "roArray" then m.teams = res.data
    if t.request.on then m.note.text = "Following " + t.request.name + ": their games get their own row and alerts." else m.note.text = "Stopped following " + t.request.name + "."
    build()
end sub

' --- The panel ------------------------------------------------------------------

sub clearPanel()
    m.panel.removeChildrenIndex(m.panel.getChildCount(), 0)
end sub

function addLabel(text as string, x as integer, y as integer, w as integer, bold as boolean, size as integer, color as string, align = "left" as string) as object
    l = m.panel.createChild("Label")
    l.text = text
    l.translation = [x, y]
    l.width = w
    l.horizAlign = align
    l.color = color
    f = CreateObject("roSGNode", "Font")
    if bold then f.uri = "pkg:/fonts/Inter-Bold.otf" else f.uri = "pkg:/fonts/Inter-Regular.otf"
    f.size = size
    l.font = f
    return l
end function

' A table: the first column wide and left, the rest as wide as fits; up to
' maxRows rows. highlight marks rows (by index) in the accent colour.
sub drawTable(title as string, header as object, rows as object, firstWidth as integer, highlight = {} as object)
    addLabel(title, 0, 0, 1040, true, 30, "0xF2F4F7FF")
    y = 56
    cols = header.Count() - 1
    for each r in rows
        if r.Count() - 1 > cols then cols = r.Count() - 1
    end for
    if cols > 10 then cols = 10
    colW = 80
    if cols > 0 then colW = Int((1040 - firstWidth) / cols)
    if colW > 160 then colW = 160
    if header.Count() > 0
        for c = 0 to cols
            if c < header.Count()
                if c = 0 then addLabel(header[0], 0, y, firstWidth - 10, true, 22, "0x98A1ADFF") else addLabel(header[c], firstWidth + (c - 1) * colW, y, colW, true, 22, "0x98A1ADFF", "right")
            end if
        end for
        y = y + 38
    end if
    maxRows = 12
    if header.Count() = 0 then maxRows = 13
    for i = 0 to rows.Count() - 1
        if i >= maxRows
            addLabel("and " + (rows.Count() - maxRows).ToStr() + " more", 0, y, 1040, false, 22, "0x98A1ADFF")
            exit for
        end if
        r = rows[i]
        color = "0xF2F4F7FF"
        if highlight.DoesExist(i.ToStr()) then color = "0x2DD4BFFF"
        for c = 0 to cols
            if c < r.Count()
                if c = 0 then addLabel(r[0], 0, y, firstWidth - 10, false, 26, color) else addLabel(r[c], firstWidth + (c - 1) * colW, y, colW, false, 26, color, "right")
            end if
        end for
        y = y + 40
    end for
end sub

sub drawMessage(title as string, text as string)
    addLabel(title, 0, 0, 1040, true, 30, "0xF2F4F7FF")
    l = addLabel(text, 0, 60, 1040, false, 28, "0x98A1ADFF")
    l.wrap = true
end sub

sub drawSection()
    clearPanel()
    e = m.focusedSection
    if e = invalid then return
    g = m.game
    shown = scoreShown(g)
    sec = e.section
    if sec = "none"
        if m.sumError <> ""
            drawMessage("Summary", "Couldn't load this game's summary: " + m.sumError)
        else if m.sum = invalid
            drawMessage("Summary", "Loading...")
        else
            drawMessage("Summary", "ESPN has no box score for this game yet.")
        end if
        return
    end if
    ' Everything but the standings gives the score away.
    if not shown and sec <> "standings" and getOr(g, "state", "pre") <> "pre"
        drawMessage(e.label, "Spoiler-free is on. Choose Show the score to see this game's score and stats.")
        return
    end if
    if sec = "lines"
        n = 0
        for each s in [g.away, g.home]
            c = getOr(s, "lines", []).Count()
            if c > n then n = c
        end for
        header = [""]
        for i = 1 to n
            header.Push(i.ToStr())
        end for
        header.Push("T")
        rows = []
        for each s in [g.away, g.home]
            r = [sideName(s)]
            lines = getOr(s, "lines", [])
            for i = 0 to n - 1
                if i < lines.Count() then r.Push(asString(lines[i])) else r.Push("")
            end for
            r.Push(getOr(s, "score", ""))
            rows.Push(r)
        end for
        ' Long games (extra innings) show their last periods.
        while header.Count() > 11
            header.Delete(1)
            for each r in rows
                r.Delete(1)
            end for
        end while
        drawTable("Score by period", header, rows, 260)
    else if sec = "race"
        rows = []
        for each d in getOr(g, "race", [])
            num = getOr(d, "number", "")
            if num <> "" then num = "#" + num
            rows.Push([asString(d.place) + ".  " + getOr(d, "name", ""), num])
        end for
        drawTable("Running order", [], rows, 860)
    else if sec = "leaders"
        rows = []
        for each l in getOr(m.sum, "leaders", [])
            rows.Push([getOr(l, "title", ""), leaderText(getOr(l, "away", invalid)), leaderText(getOr(l, "home", invalid))])
        end for
        drawLeaders(rows)
    else if sec = "stats"
        rows = []
        for each s in getOr(m.sum, "stats", [])
            rows.Push([getOr(s, "label", ""), getOr(s, "away", ""), getOr(s, "home", "")])
        end for
        drawTable("Team stats", ["", sideName(g.away), sideName(g.home)], rows, 560)
    else if sec = "players"
        t = m.sum.players[e.index]
        header = [""]
        header.Append(getOr(t, "labels", []))
        rows = []
        for each p in getOr(t, "rows", [])
            r = [joinStrings([getOr(p, "name", ""), getOr(p, "position", "")], " ")]
            r.Append(getOr(p, "stats", []))
            rows.Push(r)
        end for
        drawTable(e.label, header, rows, 340)
    else if sec = "standings"
        t = m.sum.standings[e.index]
        header = [""]
        header.Append(getOr(t, "labels", []))
        rows = []
        mark = {}
        for each r in getOr(t, "rows", [])
            id = getOr(r, "id", "")
            if id <> "" and (id = getOr(g.away, "id", "") or id = getOr(g.home, "id", "")) then mark[rows.Count().ToStr()] = true
            row = [getOr(r, "name", "")]
            row.Append(getOr(r, "stats", []))
            rows.Push(row)
        end for
        drawTable(e.label, header, rows, 420, mark)
    end if
end sub

' Leaders as three columns: what, the away side's, the home side's.
sub drawLeaders(rows as object)
    g = m.game
    addLabel("Leaders", 0, 0, 1040, true, 30, "0xF2F4F7FF")
    addLabel(sideName(g.away), 260, 56, 380, true, 22, "0x98A1ADFF")
    addLabel(sideName(g.home), 660, 56, 380, true, 22, "0x98A1ADFF")
    y = 94
    for each r in rows
        if y > 540 then exit for
        addLabel(r[0], 0, y, 250, false, 24, "0x98A1ADFF")
        a = addLabel(r[1], 260, y, 380, false, 26, "0xF2F4F7FF")
        a.wrap = true
        a.maxLines = 2
        h = addLabel(r[2], 660, y, 380, false, 26, "0xF2F4F7FF")
        h.wrap = true
        h.maxLines = 2
        y = y + 80
    end for
end sub

function leaderText(l as dynamic) as string
    if l = invalid then return ""
    return joinStrings([getOr(l, "shortName", getOr(l, "name", "")), getOr(l, "value", "")], "  ")
end function

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "play"
        chans = getOr(m.game, "channels", [])
        if chans.Count() > 0
            m.top.navigate = { screen: "LivePlayerScreen", params: { channels: chans, index: 0 } }
        else
            m.note.text = "This game isn't on any of your channels."
        end if
        return true
    end if
    return false
end function
