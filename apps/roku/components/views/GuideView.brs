' The Live TV guide, like a cable box's: Roku's TimeGrid with six hours of
' programs from the last half hour on, what's focused described up top, and
' the last channel watched previewing while the guide has focus. OK watches
' the channel full screen; Options (*) favourites it, follows or records the
' show, and picks which channels are shown.

sub init()
    m.grid = m.top.findNode("grid")
    m.status = m.top.findNode("status")
    m.note = m.top.findNode("note")
    m.preview = m.top.findNode("preview")
    m.spinner = m.top.findNode("spinner")
    m.spinner.poster.uri = "pkg:/images/spinner.png"
    m.menu = m.top.findNode("menu")
    m.grid.observeField("programFocusedDetails", "onProgramFocused")
    m.grid.observeField("programSelected", "onProgramSelected")
    m.menu.observeField("chosen", "onMenuChosen")
    m.menu.observeField("closed", "onMenuClosed")
    m.preview.observeField("state", "onPreviewState")
    m.top.observeField("refresh", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.reload = m.top.findNode("reload")
    m.reload.observeField("fire", "load")
    m.reload.control = "start"

    m.guide = invalid ' the last answer: recordings, canRecord, ...
    m.shown = [] ' channels in grid order
    m.filters = []
    m.filter = "all"
    m.sel = invalid ' { index, channel, program (invalid in a gap), start, stop }
    m.focusId = 0 ' the channel to keep focused across reloads
    m.followLast = false ' after watching: focus the last channel watched instead
    m.prefs = invalid
    m.loading = false
    m.loaded = 0
    ' The preview: on while the guide has focus.
    m.previewOn = false
    m.tunedId = 0
    m.viewerId = ""
    m.joinCount = 0
    m.menuLevel = ""
    m.menuActions = []
    m.menuMsg = ""
    m.status.text = "Loading the guide..."
    load()
end sub

sub onFocusChanged()
    if m.top.hasFocus()
        if m.menu.visible then m.menu.setFocus(true) else m.grid.setFocus(true)
    end if
    inChain = m.top.isInFocusChain()
    if inChain and not m.previewOn
        startPreview()
    else if not inChain and m.previewOn
        stopPreview()
    end if
end sub

' --- Loading -------------------------------------------------------------------

function floorHalf(t as integer) as integer
    return Int(t / 1800) * 1800
end function

function nowSecs() as integer
    return CreateObject("roDateTime").AsSeconds()
end function

sub load()
    if m.loading then return
    m.loading = true
    from = floorHalf(nowSecs())
    api("GET", "/api/live/guide?from=" + from.ToStr() + "&to=" + (from + 6 * 3600).ToStr(), invalid, "onLoaded", { build: "guide", filter: m.filter, timeout: 30000 })
    api("GET", "/api/me/prefs", invalid, "onPrefs")
end sub

sub onPrefs(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok and type(res.data) = "roAssociativeArray" then m.prefs = res.data
end sub

sub onLoaded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    m.loading = false
    res = event.getData()
    if not res.ok
        if m.loaded = 0 then m.status.text = res.error
        return
    end if
    m.loaded = m.loaded + 1
    m.guide = res.data
    m.shown = getOr(res, "shown", [])
    m.filters = getOr(res, "filters", [])
    m.filter = getOr(res, "filter", "all")
    if getOr(res.data, "channels", []).Count() = 0
        m.status.text = "No channels yet. An admin adds them in BingeBandit's Live TV settings."
    else if m.shown.Count() = 0
        m.status.text = "No channels in " + filterLabel(m.filter) + " yet. Press * to show others."
    else
        m.status.text = ""
    end if
    showNote("")
    ' Keep the focused channel; the first time, the last one watched.
    want = m.focusId
    if want = 0 or m.followLast then want = lastChannel()
    m.followLast = false
    m.grid.contentStartTime = res.data.from
    m.grid.content = t.content
    i = channelIndex(want)
    if i < 0 then i = 0
    if m.shown.Count() > 0 then m.grid.jumpToChannel = i
    if m.previewOn and m.tunedId = 0 then startPreview()
    ' A favourite or recording just changed: show the choices as they are now.
    if m.menu.visible and m.menuLevel = "main" and m.sel <> invalid then openMenu("main", m.menuMsg)
end sub

function lastChannel() as integer
    v = m.global.lastChannel
    if v = invalid then return 0
    return v
end function

function channelIndex(id as integer) as integer
    for i = 0 to m.shown.Count() - 1
        if m.shown[i].id = id then return i
    end for
    return -1
end function

function filterLabel(key as string) as string
    for each f in m.filters
        if f.key = key then return f.label
    end for
    return "All channels"
end function

' --- The focused program ---------------------------------------------------------

sub onProgramFocused()
    d = m.grid.programFocusedDetails
    if d = invalid or m.grid.content = invalid then return
    ci = getOr(d, "focusChannelIndex", 0)
    pi = getOr(d, "focusIndex", 0)
    row = m.grid.content.getChild(ci)
    if row = invalid or ci >= m.shown.Count() then return
    node = row.getChild(pi)
    if node = invalid then return
    p = invalid
    if not node.gap then p = node.program
    m.sel = { index: ci, channel: m.shown[ci], program: p, start: node.playStart, stop: node.playStart + node.playDuration }
    m.focusId = m.sel.channel.id
    showProgram()
end sub

sub showProgram()
    s = m.sel
    c = s.channel
    p = s.program
    where = asString(c.number) + "  " + getOr(c, "name", "")
    if p = invalid
        m.top.findNode("pTitle").text = getOr(c, "name", "")
        m.top.findNode("pMeta").text = where
        m.top.findNode("pSub").text = ""
        m.top.findNode("pDesc").text = "No guide information for this time."
        return
    end if
    title = getOr(p, "title", "")
    if getOr(p, "year", 0) > 0 then title = title + " (" + p.year.ToStr() + ")"
    m.top.findNode("pTitle").text = title
    meta = [dayName(p.start) + " " + wallClock(p.start) + " - " + wallClock(p.stop), where, episodeLabel(getOr(p, "episode", "")), getOr(p, "rating", "")]
    status = recordingStatus(c, p)
    if status <> "" then meta.Push(status)
    m.top.findNode("pMeta").text = joinStrings(meta, "  ·  ")
    m.top.findNode("pSub").text = getOr(p, "subtitle", "")
    m.top.findNode("pDesc").text = getOr(p, "description", "")
end sub

' "Today", "Tomorrow" or the weekday, in the Roku's time zone.
function dayName(unix as integer) as string
    d = CreateObject("roDateTime")
    d.FromSeconds(unix)
    d.ToLocalTime()
    n = CreateObject("roDateTime")
    n.ToLocalTime()
    ' Whole local days since the epoch, so midnight is where the day turns.
    dayOf = Int(d.AsSeconds() / 86400)
    today = Int(n.AsSeconds() / 86400)
    if dayOf = today then return "Today"
    if dayOf = today + 1 then return "Tomorrow"
    return d.GetWeekday()
end function

' "S2 E5" from the guide's "1.4.0/1" (xmltv_ns, counted from 0) or "S02E05".
function episodeLabel(ep as string) as string
    if ep = "" then return ""
    parts = ep.Split(".")
    if parts.Count() >= 2 and parts[0].Trim() <> ""
        s = Val(parts[0].Split("/")[0]) + 1
        out = "S" + Int(s).ToStr()
        if parts[1].Trim() <> "" then out = out + " E" + Int(Val(parts[1].Split("/")[0]) + 1).ToStr()
        return out
    end if
    return ep
end function

function recordingStatus(c as object, p as object) as string
    recs = getOr(m.guide, "recordings", invalid)
    if type(recs) <> "roAssociativeArray" then return ""
    s = recs[getOr(c, "epgId", "") + ":" + asString(p.start)]
    if s = invalid then return ""
    names = { pending: "Waiting for approval", scheduled: "Set to record", recording: "Recording now", completed: "Recorded", failed: "Recording failed" }
    if names.DoesExist(s) then return names[s]
    return s
end function

sub showNote(text as string)
    if text = "" and m.shown.Count() > 0
        text = filterLabel(m.filter) + "  ·  OK watches  ·  * Options"
    end if
    m.note.text = text
end sub

' --- Watching ------------------------------------------------------------------

sub onProgramSelected()
    d = m.grid.programFocusedDetails
    ci = getOr(d, "focusChannelIndex", m.grid.channelSelected)
    watch(ci)
end sub

sub watch(i as integer)
    if i < 0 or i >= m.shown.Count() then return
    stopPreview()
    ' Back from watching, the guide follows the channel flipped to.
    m.followLast = true
    m.top.navigate = { screen: "LivePlayerScreen", params: { channels: m.shown, index: i } }
end sub

' --- The preview ---------------------------------------------------------------

' Plays the last channel watched (or the first one) small, as a cable box
' does, while the guide has focus. Browsing doesn't change it.
sub startPreview()
    m.previewOn = true
    if m.shown.Count() = 0 then return
    i = channelIndex(lastChannel())
    if i < 0 then i = channelIndex(m.tunedId)
    if i < 0 then i = 0
    c = m.shown[i]
    m.tunedId = c.id
    m.top.findNode("tuned").text = asString(c.number) + "  " + getOr(c, "name", "")
    m.top.findNode("tunedBg").visible = true
    m.spinner.visible = true
    m.spinner.control = "start"
    m.joinCount = m.joinCount + 1
    api("POST", "/api/live/channels/" + c.id.ToStr() + "/watch", {}, "onPreviewJoined", { timeout: 40000, join: m.joinCount })
end sub

sub onPreviewJoined(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if t.request.join <> m.joinCount or not m.previewOn
        ' Left the guide (or the channel changed) while it was joining.
        if res.ok then apiSend("DELETE", "/api/live/viewers/" + res.data.viewerId, invalid)
        return
    end if
    if not res.ok
        stopSpinner()
        return
    end if
    m.viewerId = res.data.viewerId
    c = CreateObject("roSGNode", "ContentNode")
    c.url = withToken(m.global.server + res.data.playlist)
    c.streamFormat = "hls"
    c.live = true
    c.HttpHeaders = authHeaders()
    m.preview.content = c
    m.preview.control = "play"
end sub

sub onPreviewState()
    s = m.preview.state
    if s = "playing" or s = "error" or s = "stopped" then stopSpinner()
end sub

sub stopSpinner()
    m.spinner.visible = false
    m.spinner.control = "stop"
end sub

sub stopPreview()
    m.previewOn = false
    m.joinCount = m.joinCount + 1
    m.preview.control = "stop"
    m.preview.content = invalid
    stopSpinner()
    if m.viewerId <> ""
        apiSend("DELETE", "/api/live/viewers/" + m.viewerId, invalid)
        m.viewerId = ""
    end if
end sub

' --- Options ------------------------------------------------------------------

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "options" and m.sel <> invalid and not m.menu.visible
        openMenu("main", "")
        return true
    end if
    return false
end function

function isFollowing(title as string) as boolean
    for each f in getOr(m.prefs, "liveFollows", [])
        if LCase(f) = LCase(title) then return true
    end for
    return false
end function

sub openMenu(level as string, message as string)
    m.menuLevel = level
    labels = []
    m.menuActions = []
    focus = 0
    s = m.sel
    c = s.channel
    p = s.program
    if level = "main"
        if p <> invalid then m.menu.title = getOr(p, "title", "") else m.menu.title = getOr(c, "name", "")
        labels.Push("Watch " + getOr(c, "name", ""))
        m.menuActions.Push({ act: "watch" })
        if isTrue(c.favorite) then labels.Push("Remove from favourites") else labels.Push("Add to favourites")
        m.menuActions.Push({ act: "favorite" })
        if p <> invalid and m.prefs <> invalid
            if isFollowing(getOr(p, "title", "")) then labels.Push("Following: stop alerts") else labels.Push("Follow show")
            m.menuActions.Push({ act: "follow" })
        end if
        status = recordingStatus(c, p)
        if p <> invalid and isTrue(m.guide.canRecord) and getOr(p, "id", 0) > 0 and p.stop > nowSecs() and status = ""
            ask = isTrue(m.guide.recordNeedsApproval)
            if ask then labels.Push("Ask to record") else labels.Push("Record")
            m.menuActions.Push({ act: "record", series: false })
            if ask then labels.Push("Ask to record the series") else labels.Push("Record series")
            m.menuActions.Push({ act: "record", series: true })
        end if
        if m.filters.Count() > 1
            labels.Push("Channels shown: " + filterLabel(m.filter))
            m.menuActions.Push({ act: "level", level: "filter" })
        end if
        m.menu.message = message
        m.menuMsg = message
        ' Redrawn after a choice: stay on it.
        if message <> "" and m.menu.visible then focus = m.menu.chosen
    else if level = "filter"
        m.menu.title = "Channels shown"
        m.menu.message = ""
        for each f in m.filters
            if f.key = m.filter then focus = labels.Count()
            labels.Push(f.label)
            m.menuActions.Push({ act: "filter", key: f.key })
        end for
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
    s = m.sel
    c = s.channel
    p = s.program
    if a.act = "level"
        openMenu(a.level, "")
    else if a.act = "watch"
        closeMenu()
        watch(s.index)
    else if a.act = "favorite"
        if isTrue(c.favorite) then method = "DELETE" else method = "POST"
        api(method, "/api/live/channels/" + c.id.ToStr() + "/favorite", invalid, "onFavorite")
    else if a.act = "follow"
        ' Fresh preferences, so nothing set elsewhere meanwhile is lost.
        api("GET", "/api/me/prefs", invalid, "onFollowPrefs", { title: getOr(p, "title", "") })
    else if a.act = "record"
        api("POST", "/api/dvr/recordings", { channelId: c.id, programId: p.id, series: a.series }, "onRecorded", { title: getOr(p, "title", ""), series: a.series })
    else if a.act = "filter"
        closeMenu()
        m.filter = a.key
        m.status.text = ""
        load()
    end if
end sub

sub onFavorite(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        menuMessage(res.error)
        return
    end if
    c = m.sel.channel
    if isTrue(c.favorite)
        c.favorite = false
        menuMessage(getOr(c, "name", "") + " is no longer a favourite.")
    else
        c.favorite = true
        menuMessage(getOr(c, "name", "") + " is in Favourites now.")
    end if
    load()
end sub

sub onFollowPrefs(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok or type(res.data) <> "roAssociativeArray"
        menuMessage(res.error)
        return
    end if
    prefs = res.data
    title = t.request.title
    follows = getOr(prefs, "liveFollows", [])
    if type(follows) <> "roArray" then follows = []
    nextList = []
    following = false
    for each f in follows
        if LCase(f) = LCase(title) then following = true else nextList.Push(f)
    end for
    if not following then nextList.Push(title)
    prefs.liveFollows = nextList
    api("PUT", "/api/me/prefs", prefs, "onFollowed", { title: title, following: not following, prefs: prefs })
end sub

sub onFollowed(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        menuMessage(res.error)
        return
    end if
    m.prefs = t.request.prefs
    if t.request.following
        menuMessage("You'll get a pop-up before " + Chr(34) + t.request.title + Chr(34) + " comes on.")
    else
        menuMessage("You'll no longer hear about " + Chr(34) + t.request.title + Chr(34) + ".")
    end if
end sub

sub onRecorded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        menuMessage(res.error)
        return
    end if
    title = Chr(34) + t.request.title + Chr(34)
    if getOr(res.data, "status", "") = "pending"
        text = "Sent to an admin to approve."
    else if t.request.series
        text = "Recording every " + title + "."
    else
        text = title + " will be recorded."
    end if
    conflict = getOr(res.data, "conflict", "")
    if conflict <> "" then text = text + " It may not record: " + conflict + "."
    menuMessage(text)
    load()
end sub

' Says what happened and redraws the choices (a favourite or recording changes them).
sub menuMessage(text as string)
    if m.menu.visible and m.menuLevel = "main" then openMenu("main", text) else showNote(text)
end sub

sub onMenuClosed()
    if m.menuLevel <> "main" then openMenu("main", "") else closeMenu()
end sub

sub closeMenu()
    m.menu.visible = false
    m.menuLevel = ""
    m.grid.setFocus(true)
end sub
