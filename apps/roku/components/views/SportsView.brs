' The Sports Hub on Roku: a day of every league's games in rows (Live now,
' Your teams, then each league), the focused game described up top. OK opens
' a game's details (box score, stats, leaders, standings, follow its teams);
' Play watches it on your Live TV channel. Options (*) picks the day and the
' sport, shows only games on your channels, turns spoiler-free mode on or off
' and picks your teams. Scores refresh every half minute while it's shown.

sub init()
    m.rows = m.top.findNode("rows")
    m.status = m.top.findNode("status")
    m.note = m.top.findNode("note")
    m.menu = m.top.findNode("menu")
    m.rows.observeField("rowItemSelected", "onSelected")
    m.rows.observeField("rowItemFocused", "onFocused")
    m.menu.observeField("chosen", "onMenuChosen")
    m.menu.observeField("closed", "onMenuClosed")
    m.top.observeField("refresh", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.reload = m.top.findNode("reload")
    m.reload.observeField("fire", "onReloadTimer")
    m.reload.control = "start"
    m.noteTimer = m.top.findNode("noteTimer")
    m.noteTimer.observeField("fire", "clearNote")
    m.top.findNode("hint").text = "* Day, sport, spoiler-free, your teams"

    m.offset = 0
    m.sport = "all"
    m.withChannel = false
    m.teams = [] ' the viewer's teams, as /api/sports/teams has them
    m.leagues = []
    m.sports = []
    m.prefs = invalid
    m.loading = false
    m.loaded = 0
    m.focusKey = invalid ' { row, game } kept across reloads
    m.menuLevel = ""
    m.menuActions = []
    m.lastAct = ""
    m.menuLeague = invalid
    m.leagueTeams = []
    m.status.text = "Loading scores..."
    drawDay()
    load()
end sub

sub onFocusChanged()
    if m.top.hasFocus()
        if m.menu.visible then m.menu.setFocus(true) else m.rows.setFocus(true)
    end if
end sub

' Only while it's on screen: Home keeps its panels when another is picked or
' a screen opens on top.
sub onReloadTimer()
    n = m.top
    while n <> invalid
        if not n.visible then return
        n = n.getParent()
    end while
    if not m.menu.visible then load()
end sub

sub drawDay()
    d = dayLabel(m.offset)
    if m.offset >= -1 and m.offset <= 1
        t = CreateObject("roDateTime")
        t.FromSeconds(t.AsSeconds() + m.offset * 86400)
        t.ToLocalTime()
        d = d + "  ·  " + shortDate(t)
    end if
    if m.sport <> "all" then d = d + "  ·  " + sportName(m.sport)
    m.top.findNode("day").text = d
end sub

sub load()
    if m.loading then return
    m.loading = true
    api("GET", "", invalid, "onLoaded", { build: "sports", date: dayKey(m.offset), today: m.offset = 0, sport: m.sport, withChannel: m.withChannel, offset: m.offset })
end sub

sub onLoaded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    m.loading = false
    res = event.getData()
    if t.request.offset <> m.offset then
        load() ' another day was picked meanwhile
        return
    end if
    if not res.ok
        if m.loaded = 0 then m.status.text = res.error else showNote(res.error)
        return
    end if
    m.loaded = m.loaded + 1
    if res.prefs <> invalid
        m.prefs = res.prefs
        m.global.spoilerFree = isTrue(res.prefs.spoilerFree)
    end if
    m.teams = getOr(res, "teams", [])
    m.leagues = getOr(res, "leagues", [])
    m.sports = getOr(res, "sports", [])
    err = getOr(res.data, "error", "")
    if err <> "" then showNote(err)
    content = t.content
    if content = invalid or content.getChildCount() = 0
        m.rows.content = invalid
        m.status.text = emptyText()
        describe(invalid)
        return
    end if
    m.status.text = ""
    ' Keep the place: the same game in the same row, else the same row.
    want = m.focusKey
    m.rows.content = content
    if want <> invalid
        for r = 0 to content.getChildCount() - 1
            row = content.getChild(r)
            if row.title = want.row
                col = 0
                for i = 0 to row.getChildCount() - 1
                    if asString(row.getChild(i).game.id) = want.game then col = i
                end for
                m.rows.jumpToRowItem = [r, col]
                exit for
            end if
        end for
    end if
    onFocused()
end sub

function emptyText() as string
    text = "No games " + LCase(dayLabel(m.offset))
    if m.offset < -1 or m.offset > 1 then text = "No games on " + dayLabel(m.offset)
    if m.sport <> "all" then text = text + " in " + sportName(m.sport)
    if m.withChannel then text = text + " on your channels"
    return text + ". Press * to pick another day."
end function

function focusedContent() as dynamic
    f = m.rows.rowItemFocused
    if f = invalid or f.Count() < 2 or m.rows.content = invalid then return invalid
    row = m.rows.content.getChild(f[0])
    if row = invalid then return invalid
    return row.getChild(f[1])
end function

sub onFocused()
    c = focusedContent()
    if c <> invalid
        m.focusKey = { row: c.getParent().title, game: asString(c.game.id) }
        describe(c)
    end if
end sub

' The focused game up top: its full name, how it stands and where it's on.
sub describe(c as dynamic)
    name = m.top.findNode("gName")
    meta = m.top.findNode("gMeta")
    more = m.top.findNode("gMore")
    if c = invalid
        name.text = ""
        meta.text = ""
        more.text = ""
        return
    end if
    g = c.game
    shown = scoreShown(g)
    if isRace(g)
        name.text = getOr(g, "name", g.shortName)
    else
        a = sideName(g.away, true)
        h = sideName(g.home, true)
        if shown
            a = a + " " + getOr(g.away, "score", "")
            h = h + " " + getOr(g.home, "score", "")
        end if
        name.text = a + "  at  " + h
    end if
    state = getOr(g, "state", "pre")
    status = gameStatus(g)
    if state <> "pre" and not shown then status = "Score hidden (spoiler-free)"
    if state = "in" and shown then status = "Live  ·  " + status
    meta.text = joinStrings([c.leagueName, status, getOr(g, "venue", "")], "  ·  ")
    lines = []
    ch = channelsText(g)
    if ch <> ""
        lines.Push("On your channels: " + ch + ". Play watches it.")
    else if getOr(g, "broadcasts", []).Count() > 0
        lines.Push("TV: " + joinStrings(g.broadcasts, ", "))
    end if
    if shown and not isRace(g)
        leaders = []
        for each s in [g.away, g.home]
            ld = getOr(s, "leader", invalid)
            if ld <> invalid then leaders.Push(joinStrings([getOr(ld, "shortName", getOr(ld, "name", "")), getOr(ld, "value", "")], " "))
        end for
        if leaders.Count() > 0 then lines.Push(joinStrings(leaders, "   ·   "))
    else if state = "pre" and not isRace(g)
        ' Probable starters (baseball) come as each side's leader before the game.
        probables = []
        for each s in [g.away, g.home]
            ld = getOr(s, "leader", invalid)
            if ld <> invalid then probables.Push(getOr(ld, "shortName", getOr(ld, "name", "")))
        end for
        if probables.Count() = 2 then lines.Push("Probable: " + joinStrings(probables, " vs "))
    end if
    more.text = joinStrings(lines, Chr(10))
end sub

sub onSelected()
    c = focusedContent()
    if c = invalid then return
    m.top.navigate = { screen: "GameScreen", params: { game: c.game, leagueName: c.leagueName, sport: c.sport } }
end sub

sub watch(g as object)
    chans = getOr(g, "channels", [])
    if chans.Count() = 0
        showNote("That game isn't on any of your channels.")
        return
    end if
    m.top.navigate = { screen: "LivePlayerScreen", params: { channels: chans, index: 0 } }
end sub

sub showNote(text as string)
    m.note.text = text
    m.noteTimer.control = "stop"
    if text <> "" then m.noteTimer.control = "start"
end sub

sub clearNote()
    m.note.text = ""
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if m.menu.visible then return false
    if key = "options"
        openMenu("main", "")
        return true
    end if
    if key = "play"
        c = focusedContent()
        if c <> invalid then watch(c.game)
        return true
    end if
    return false
end function

' --- Options ------------------------------------------------------------------

function onOff(v as boolean) as string
    if v then return "On"
    return "Off"
end function

function following(league as string, id as string) as boolean
    for each t in m.teams
        if t.league = league and t.id = id then return true
    end for
    return false
end function

function teamCount(league as string) as integer
    n = 0
    for each t in m.teams
        if t.league = league then n = n + 1
    end for
    return n
end function

sub openMenu(level as string, message as string)
    m.menuLevel = level
    labels = []
    m.menuActions = []
    focus = 0
    m.menu.message = message
    if level = "main"
        m.menu.title = "Sports"
        c = focusedContent()
        if c <> invalid
            g = c.game
            for each ch in getOr(g, "channels", [])
                labels.Push("Watch on " + joinStrings([asString(ch.number), getOr(ch, "name", "")], " "))
                m.menuActions.Push({ act: "watch" })
                exit for
            end for
            labels.Push("Game details")
            m.menuActions.Push({ act: "details" })
            if getOr(g, "state", "pre") <> "pre" and not scoreShown(g)
                labels.Push("Show the score")
                m.menuActions.Push({ act: "reveal" })
            end if
        end if
        labels.Push("Day: " + dayLabel(m.offset))
        m.menuActions.Push({ act: "level", level: "day" })
        if m.sport = "all" then s = "All sports" else s = sportName(m.sport)
        labels.Push("Sport: " + s)
        m.menuActions.Push({ act: "level", level: "sport" })
        if getOr(m.global.user, "liveTV", true) <> false
            labels.Push("Only games on my channels: " + onOff(m.withChannel))
            m.menuActions.Push({ act: "withChannel" })
        end if
        labels.Push("Spoiler-free: " + onOff(isTrue(m.global.spoilerFree)))
        m.menuActions.Push({ act: "spoilers" })
        labels.Push("Your teams (" + m.teams.Count().ToStr() + ")")
        m.menuActions.Push({ act: "level", level: "leagues" })
        ' Redrawn after a choice: stay on it, wherever it moved to.
        if message <> "" and m.menu.visible
            for i = 0 to m.menuActions.Count() - 1
                if m.menuActions[i].act = m.lastAct then focus = i
            end for
        end if
    else if level = "day"
        m.menu.title = "Day"
        for o = -3 to 7
            if o = m.offset then focus = labels.Count()
            labels.Push(dayLabel(o))
            m.menuActions.Push({ act: "day", offset: o })
        end for
    else if level = "sport"
        m.menu.title = "Sport"
        labels.Push("All sports")
        m.menuActions.Push({ act: "sport", sport: "all" })
        for each s in m.sports
            if s = m.sport then focus = labels.Count()
            labels.Push(sportName(s))
            m.menuActions.Push({ act: "sport", sport: s })
        end for
    else if level = "leagues"
        m.menu.title = "Your teams"
        if message = "" then m.menu.message = "Your teams' games get their own row, alerts and Game Lights."
        for each l in m.leagues
            if hasTeams(l.sport)
                n = teamCount(l.id)
                label = l.name
                if n > 0 then label = label + " (" + n.ToStr() + ")"
                if m.menuLeague <> invalid and m.menuLeague.id = l.id then focus = labels.Count()
                labels.Push(label)
                m.menuActions.Push({ act: "league", league: l })
            end if
        end for
    else if level = "teams"
        m.menu.title = m.menuLeague.name
        if message = "" then m.menu.message = "OK follows a team or stops following it."
        ' Followed teams first.
        for pass = 0 to 1
            for each t in m.leagueTeams
                f = following(m.menuLeague.id, t.id)
                if (pass = 0 and f) or (pass = 1 and not f)
                    label = getOr(t, "name", "")
                    if f then label = label + "  ·  following"
                    labels.Push(label)
                    m.menuActions.Push({ act: "team", team: t })
                end if
            end for
        end for
        if labels.Count() = 0 and message = "" then m.menu.message = "Loading teams..."
        if message <> "" and m.menu.visible then focus = m.menu.chosen
    end if
    m.menu.focus = focus
    m.menu.options = labels
    if not m.menu.visible
        m.menu.visible = true
        m.menu.setFocus(true)
    end if
end sub

sub onMenuChosen()
    a = m.menuActions[m.menu.chosen]
    if a = invalid then return
    m.lastAct = a.act
    if a.act = "level"
        openMenu(a.level, "")
    else if a.act = "watch"
        c = focusedContent()
        closeMenu()
        if c <> invalid then watch(c.game)
    else if a.act = "details"
        closeMenu()
        onSelected()
    else if a.act = "reveal"
        c = focusedContent()
        closeMenu()
        if c <> invalid
            r = m.global.revealed
            r[asString(c.game.id)] = true
            m.global.revealed = r
            ' Redraw the tiles with the score showing.
            load()
        end if
    else if a.act = "day"
        closeMenu()
        m.offset = a.offset
        m.focusKey = invalid
        changed()
    else if a.act = "sport"
        closeMenu()
        m.sport = a.sport
        changed()
    else if a.act = "withChannel"
        m.withChannel = not m.withChannel
        openMenu("main", "")
        changed()
    else if a.act = "spoilers"
        api("GET", "/api/me/prefs", invalid, "onSpoilerPrefs")
    else if a.act = "league"
        m.menuLeague = a.league
        m.leagueTeams = []
        openMenu("teams", "")
        api("GET", "/api/sports/" + a.league.id + "/teams", invalid, "onLeagueTeams", { league: a.league.id, timeout: 30000 })
    else if a.act = "team"
        toggleTeam(a.team)
    end if
end sub

' The rows again for a new day, sport or filter.
sub changed()
    drawDay()
    m.status.text = "Loading scores..."
    m.rows.content = invalid
    describe(invalid)
    m.loading = false
    load()
end sub

sub onSpoilerPrefs(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok or type(res.data) <> "roAssociativeArray"
        openMenu("main", res.error)
        return
    end if
    prefs = res.data
    prefs.spoilerFree = not isTrue(prefs.spoilerFree)
    api("PUT", "/api/me/prefs", prefs, "onSpoilerSaved", { on: prefs.spoilerFree })
end sub

sub onSpoilerSaved(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        openMenu("main", res.error)
        return
    end if
    m.global.spoilerFree = t.request.on
    if t.request.on
        msg = "Scores are hidden until you choose Show the score on a game."
    else
        msg = "Scores show again."
    end if
    if m.menu.visible and m.menuLevel = "main" then openMenu("main", msg)
    load()
end sub

sub onLeagueTeams(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    if m.menuLevel <> "teams" or m.menuLeague = invalid or m.menuLeague.id <> t.request.league then return
    res = event.getData()
    if not res.ok or type(res.data) <> "roArray"
        openMenu("teams", "Couldn't load the teams: " + asString(res.error))
        return
    end if
    m.leagueTeams = res.data
    openMenu("teams", "")
end sub

sub toggleTeam(team as object)
    league = m.menuLeague.id
    list = []
    on = not following(league, team.id)
    for each t in m.teams
        if not (t.league = league and t.id = team.id) then list.Push({ league: t.league, id: t.id })
    end for
    if on then list.Push({ league: league, id: team.id })
    api("PUT", "/api/sports/teams", list, "onTeamsSaved", { name: getOr(team, "name", ""), on: on, timeout: 30000 })
end sub

sub onTeamsSaved(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        if m.menuLevel = "teams" then openMenu("teams", res.error)
        return
    end if
    if type(res.data) = "roArray" then m.teams = res.data
    if t.request.on then msg = "Following " + t.request.name + "." else msg = "Stopped following " + t.request.name + "."
    if m.menu.visible and m.menuLevel = "teams" then openMenu("teams", msg)
    load()
end sub

' Back in a list goes up a level, then closes.
sub onMenuClosed()
    if m.menuLevel = "teams"
        openMenu("leagues", "")
    else if m.menuLevel <> "main"
        openMenu("main", "")
    else
        closeMenu()
    end if
end sub

sub closeMenu()
    m.menu.visible = false
    m.menuLevel = ""
    m.rows.setFocus(true)
end sub
