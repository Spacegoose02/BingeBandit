' The player. Direct play when roDeviceInfo says this Roku decodes the file's
' container, video and the chosen audio track; otherwise (or when direct play
' fails or stalls) the server converts it to HLS from the current position.
' The controls are our own so seeking can restart a conversion at the new
' point. Audio and subtitles follow the viewer's preferences (web Account)
' and change from Options (*); text subtitles are drawn here from WebVTT and
' image subtitles are burned in by the server. Watch Together keeps the
' player in a room with people on other screens (see the section below).

sub init()
    m.video = m.top.findNode("video")
    m.overlay = m.top.findNode("overlay")
    m.status = m.top.findNode("status")
    m.spinner = m.top.findNode("spinner")
    m.spinner.poster.uri = "pkg:/images/spinner.png"
    m.upNext = m.top.findNode("upNext")
    m.skip = m.top.findNode("skip")
    m.toast = m.top.findNode("toast")
    m.menu = m.top.findNode("menu")
    m.reportTimer = m.top.findNode("reportTimer")
    m.hideTimer = m.top.findNode("hideTimer")
    m.seekTimer = m.top.findNode("seekTimer")
    m.stallTimer = m.top.findNode("stallTimer")
    m.nextTimer = m.top.findNode("nextTimer")
    m.toastTimer = m.top.findNode("toastTimer")
    m.reportTimer.observeField("fire", "onReportTimer")
    m.hideTimer.observeField("fire", "hideOverlay")
    m.seekTimer.observeField("fire", "commitSeek")
    m.stallTimer.observeField("fire", "onStall")
    m.nextTimer.observeField("fire", "onNextTick")
    m.toastTimer.observeField("fire", "hideToast")
    m.togetherTimer = m.top.findNode("togetherTimer")
    m.reconnectTimer = m.top.findNode("reconnectTimer")
    m.reconnectTimer.observeField("fire", "onReconnectTimer")
    m.togetherTimer.observeField("fire", "pollRoom")
    m.menu.observeField("chosen", "onMenuChosen")
    m.menu.observeField("closed", "onMenuClosed")
    m.video.observeField("state", "onState")
    m.video.observeField("position", "onPosition")
    m.top.observeField("params", "onParams")
    m.top.observeField("closing", "onClosing")
    m.top.observeField("focusedChild", "onFocusChanged")

    m.item = invalid
    m.file = invalid
    m.detail = invalid
    m.prefs = invalid
    m.waiting = 0 ' item and prefs still loading
    m.mode = ""
    m.offset = 0.0 ' a conversion starts this many seconds into the file
    m.transcodeId = ""
    ' Riding out a server restart: how to carry on, from where, the attempts
    ' so far and when to give up (0 when not reconnecting).
    m.reconnectHow = ""
    m.reconnectAt = 0.0
    m.reconnectTries = 0
    m.reconnectUntil = 0
    m.transcodeRetried = false ' a failed conversion was started over once
    m.directRetryAt = -1000.0 ' where direct play last started over after an error
    m.pos = 0.0
    m.played = 0.0
    m.duration = 0.0
    m.seekTarget = -1.0
    m.finished = false
    m.checkStart = false
    m.wasPaused = false
    m.next = invalid
    ' Tracks: the audio stream (-1 the file's default), the subtitle stream
    ' shown (invalid for none), the image subtitle the server burns in, and
    ' the quality (-1 direct play when possible, 0 convert at the file's
    ' size, else convert at that height).
    m.audio = -1
    m.sub = invalid
    m.burn = -1
    m.quality = -1
    m.audioPending = false
    m.cues = []
    m.cueIdx = 0
    m.cueShown = -1
    m.subsRaised = false
    m.cueLoad = 0
    m.markers = []
    m.chapters = []
    m.introDone = false
    m.creditsDone = false
    m.skipKind = ""
    m.menuLevel = ""
    m.menuActions = []
    m.trailerFormat = ""
    m.playSession = "roku-" + CreateObject("roDeviceInfo").GetRandomUUID()
    m.di = CreateObject("roDeviceInfo")
    ' Watch Together: the room's id ("" when not in one), the newest change
    ' seen, the server clock minus ours (ms), who's in it, and the people who
    ' can be invited.
    m.room = ""
    m.roomRev = 0
    m.roomSkew = 0#
    m.roomAsked = 0#
    m.roomMembers = []
    m.roomPeople = invalid
    m.roomInvited = {}
    m.roomPolling = false
    m.roomLoad = 0
    m.roomStart = -1.0
    m.roomPaused = false
    m.pendingPause = invalid
    showBusy("Loading...")
end sub

sub onFocusChanged()
    ' Keys come to this screen; the Video node never takes focus.
end sub

sub onParams()
    if m.top.params.trailer <> invalid
        playTrailer("mp4")
        return
    end if
    m.waiting = 2
    ' Joining a watch party: where the room is decides where to start.
    room = getOr(m.top.params, "together", "")
    if room <> ""
        m.room = room
        m.waiting = 3
        if isTrue(m.top.params.announce)
            ' Moving the party on to the next episode: say so before looking.
            api("POST", "/api/together/" + room + "/state", { itemId: m.top.params.id, paused: false, position: 0 }, "onRoomFirst")
        else
            api("GET", "/api/together/" + room + "?watching=1", invalid, "onRoomFirst")
        end if
        m.roomAsked = nowMs()
    end if
    api("GET", "/api/items/" + m.top.params.id.ToStr(), invalid, "onItem")
    api("GET", "/api/me/prefs", invalid, "onPrefs")
end sub

sub onPrefs(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok and type(res.data) = "roAssociativeArray" then m.prefs = res.data
    m.waiting = m.waiting - 1
    if m.waiting = 0 then start()
end sub

sub onItem(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        m.waiting = -1
        showError(res.error)
        return
    end if
    m.detail = res.data
    m.item = res.data.item
    files = getOr(res.data, "files", [])
    if files.Count() = 0
        m.waiting = -1
        showError("This item has no playable file.")
        return
    end if
    m.file = files[0]
    m.markers = getOr(res.data, "markers", [])
    if type(m.markers) <> "roArray" then m.markers = []
    m.duration = getOr(m.file, "duration", 0)
    if m.duration <= 0 then m.duration = getOr(m.item, "runtime", 0) * 60
    api("GET", "/api/files/" + m.file.id.ToStr() + "/chapters", invalid, "onChapters")
    it = m.item
    if it.kind = "episode"
        m.top.findNode("title").text = getOr(it, "showTitle", it.title)
        m.top.findNode("subtitle").text = joinStrings([episodeCode(it), it.title], " · ")
    else
        m.top.findNode("title").text = it.title
        if it.year <> invalid then m.top.findNode("subtitle").text = it.year.ToStr()
    end if
    m.waiting = m.waiting - 1
    if m.waiting = 0 then start()
end sub

' Both the item and the viewer's preferences are in: pick the tracks and play.
sub start()
    picked = pickTracks(m.file, m.prefs)
    m.audio = picked.audio
    m.sub = picked.subtitle
    if isImageSub(m.sub) then m.burn = m.sub.index
    if isTextSub(m.sub) then loadCues()
    at = getOr(m.top.params, "start", 0)
    if m.room <> ""
        if m.roomStart >= 0 then at = m.roomStart
        m.pendingPause = m.roomPaused
        m.togetherTimer.control = "start"
    end if
    m.pos = at
    if canDirectPlay(m.file) then playDirect(at) else transcode(at)
end sub

' --- What this Roku plays as is -----------------------------------------------

function directFormat(f as object) as string
    c = LCase(getOr(f, "container", ""))
    if c = "mp4" or c = "m4v" or c = "mov" then return "mp4"
    if c = "mkv" then return "mkv"
    return ""
end function

function rokuVideoCodec(c as string) as string
    c = LCase(c)
    if c = "h264" then return "mpeg4 avc"
    if c = "hevc" or c = "vp9" or c = "av1" then return c
    return ""
end function

function rokuAudioCodec(c as string) as string
    c = LCase(c)
    if c = "aac" or c = "mp3" or c = "ac3" or c = "eac3" or c = "flac" or c = "opus" or c = "vorbis" or c = "alac" or c = "dts" then return c
    if Left(c, 4) = "pcm_" then return "lpcm"
    return ""
end function

' Whether the file plays as is with the chosen audio track. A burned-in
' subtitle always needs the server.
function canDirectPlay(f as object) as boolean
    if m.burn >= 0 then return false
    fmt = directFormat(f)
    if fmt = "" then return false
    vc = getOr(f, "videoCodec", "")
    if vc <> ""
        rc = rokuVideoCodec(vc)
        if rc = "" then return false
        r = m.di.CanDecodeVideo({ Codec: rc, Container: fmt })
        if r = invalid or not isTrue(r.result) then return false
        ' HDR on a TV without it would look washed out; the server tone maps.
        if isTrue(f.hdr)
            dp = m.di.GetDisplayProperties()
            if dp = invalid or not isTrue(dp.Hdr10) then return false
        end if
        ' A 4K file on a 1080p Roku is converted down rather than risked.
        if getOr(f, "height", 0) > displayHeight() + 120 then return false
    end if
    a = audioFor(f, m.audio)
    ac = getOr(f, "audioCodec", "")
    if a <> invalid then ac = getOr(a, "codec", ac)
    if ac = "" then return true
    rac = rokuAudioCodec(ac)
    if rac = "" then return false
    r = m.di.CanDecodeAudio({ Codec: rac })
    return r <> invalid and isTrue(r.result)
end function

' The TV output's height from the video mode, e.g. "1080p", "2160p60".
function displayHeight() as integer
    mode = m.di.GetVideoMode()
    p = Instr(1, mode, "p")
    if p = 0 then p = Instr(1, mode, "i")
    if p > 1
        h = Val(Left(mode, p - 1))
        if h > 0 then return h
    end if
    return 1080
end function

' --- Starting playback --------------------------------------------------------

sub playDirect(at as dynamic)
    stopTranscode()
    m.mode = "direct"
    m.offset = 0
    c = CreateObject("roSGNode", "ContentNode")
    c.url = withToken(m.global.server + "/api/files/" + m.file.id.ToStr() + "/direct")
    c.streamFormat = directFormat(m.file)
    c.title = m.top.findNode("title").text
    c.HttpHeaders = authHeaders()
    setBif(c, 0)
    c.PlayStart = at
    showBusy("")
    ' The Roku picks the file's default track; another one is switched to
    ' once it's playing and the tracks are known.
    m.audioPending = m.audio >= 0
    m.video.content = c
    m.video.control = "play"
end sub

sub transcode(at as dynamic)
    m.mode = "transcode"
    m.audioPending = false
    m.video.control = "stop"
    showBusy("Converting...")
    codecs = ["aac"]
    channels = 2
    for each c in ["ac3", "eac3"]
        r = m.di.CanDecodeAudio({ Codec: c })
        if r <> invalid and isTrue(r.result)
            codecs.Push(c)
            channels = 6
        end if
    end for
    maxHeight = 0
    if getOr(m.file, "height", 0) > displayHeight() then maxHeight = displayHeight()
    if m.quality > 0 then maxHeight = m.quality
    ' A picked quality encodes the video too, so a slow network or Roku gets
    ' something lighter; otherwise the server copies what it can.
    body = { start: at, maxHeight: maxHeight, audioStream: m.audio, burnSubtitle: m.burn, replace: m.transcodeId, adaptive: m.quality <= 0, encodeVideo: m.quality >= 0, audioCodecs: codecs, maxAudioChannels: channels }
    m.pendingAt = at
    api("POST", "/api/files/" + m.file.id.ToStr() + "/transcode", body, "onTranscode", { timeout: 30000 })
end sub

sub onTranscode(event as object)
    doneTask(event.getRoSGNode())
    if m.top.closing then return
    res = event.getData()
    if not res.ok
        ' The server is restarting or out of reach: wait for it.
        if serverGone(res) then lostServer("transcode", m.pendingAt) else showError(res.error)
        return
    end if
    m.transcodeId = res.data.session.id
    m.offset = m.pendingAt
    m.checkStart = true
    c = CreateObject("roSGNode", "ContentNode")
    ' fromStart asks the server for EXT-X-START so the growing EVENT playlist
    ' plays from its start, not its live edge.
    c.url = withToken(m.global.server + res.data.playlist) + "&fromStart=1"
    c.streamFormat = "hls"
    c.title = m.top.findNode("title").text
    c.HttpHeaders = authHeaders()
    setBif(c, m.offset)
    c.PlayStart = 0
    m.video.content = c
    m.video.control = "play"
end sub

' Scrub thumbnails for the trick play bar, from the server's BIF file. A
' conversion counts from where it started, so the server shifts the index.
sub setBif(c as object, from as dynamic)
    url = withToken(m.global.server + "/api/files/" + m.file.id.ToStr() + "/trickplay.bif?from=" + Int(from).ToStr())
    c.HDBifUrl = url
    c.FHDBifUrl = url
    c.SDBifUrl = url
end sub

sub stopTranscode()
    if m.transcodeId <> ""
        apiSend("DELETE", "/api/transcode/" + m.transcodeId, invalid)
        m.transcodeId = ""
    end if
end sub

' A movie's trailer file from its folder: played as is, nothing reported.
' Its container isn't known, so MP4 is tried first, then MKV.
sub playTrailer(format as string)
    t = m.top.params.trailer
    m.mode = "trailer"
    m.trailerFormat = format
    m.top.findNode("title").text = getOr(t, "title", "")
    m.top.findNode("subtitle").text = "Trailer"
    c = CreateObject("roSGNode", "ContentNode")
    c.url = withToken(absoluteUrl(t.url))
    c.streamFormat = format
    c.title = getOr(t, "title", "")
    c.HttpHeaders = authHeaders()
    showBusy("")
    m.video.content = c
    m.video.control = "play"
end sub

' Switches the Roku's own audio track in direct play. Roku lists a file's
' tracks in file order, like the server, so the choice is found by position;
' when the counts don't match, the server converts with the chosen track.
sub applyDirectAudio()
    m.audioPending = false
    tracks = m.video.availableAudioTracks
    audios = streamsOf(m.file, "audio")
    want = audioFor(m.file, m.audio)
    at = -1
    for i = 0 to audios.Count() - 1
        if want <> invalid and audios[i].index = want.index then at = i
    end for
    if tracks = invalid or at < 0 or tracks.Count() <> audios.Count()
        if m.audio >= 0 then transcode(m.pos)
        return
    end if
    m.video.audioTrack = tracks[at].Track
end sub

' --- Player events ----------------------------------------------------------

function position() as float
    return m.offset + m.video.position
end function

sub onPosition()
    p = position()
    d = p - m.pos
    if d > 0 and d < 3 and m.video.state = "playing" then m.played = m.played + d
    m.pos = p
    if m.duration <= 0 and m.video.duration > 0 then m.duration = m.video.duration
    if m.overlay.visible then drawProgress()
    drawCue()
    checkMarkers()
end sub

sub onState()
    s = m.video.state
    if s = "buffering"
        showBusy("")
        m.stallTimer.control = "start"
    else if s = "playing"
        m.stallTimer.control = "stop"
        m.reconnectUntil = 0
        m.transcodeRetried = false
        hideBusy()
        ' Some firmware joins a growing playlist near its end like a live
        ' stream; go back to where the conversion started.
        if m.checkStart
            m.checkStart = false
            if m.video.position > 20 then m.video.seek = 0
        end if
        if m.audioPending and m.mode = "direct" then applyDirectAudio()
        ' The watch party is paused: stay paused once the video is ready.
        if m.pendingPause <> invalid
            p = m.pendingPause
            m.pendingPause = invalid
            if p
                m.video.control = "pause"
                return
            end if
        end if
        m.reportTimer.control = "start"
        if m.wasPaused then report(false, false)
        m.wasPaused = false
        if m.overlay.visible then m.hideTimer.control = "start"
    else if s = "paused"
        m.wasPaused = true
        report(false, false)
        showOverlay()
        m.hideTimer.control = "stop"
    else if s = "finished"
        if m.mode = "trailer"
            m.top.navigate = { action: "back" }
            return
        end if
        finish()
    else if s = "error"
        m.stallTimer.control = "stop"
        if m.mode = "trailer" and m.trailerFormat = "mp4"
            playTrailer("mkv")
        else if m.mode = "direct" or m.mode = "transcode"
            ' Check the server is there before blaming the file: a restart
            ' ends every conversion and cuts direct play's connection.
            m.errorMode = m.mode
            m.errorAt = m.pos
            m.errorMsg = m.video.errorMsg
            showBusy("")
            api("GET", "/api/health", invalid, "onErrorHealth", { timeout: 5000 })
        else
            showError("Playback failed: " + m.video.errorMsg)
        end if
    end if
end sub

sub onErrorHealth(event as object)
    doneTask(event.getRoSGNode())
    if m.top.closing then return
    res = event.getData()
    at = m.errorAt
    if not res.ok
        lostServer(m.errorMode, at)
    else if m.errorMode = "direct"
        ' A file that had been playing most likely lost its connection (the
        ' server is already back): try it as it was, once. One that won't
        ' play, a profile or level this Roku can't decode, gets converted.
        if at > 5 and at > m.directRetryAt + 30
            m.directRetryAt = at
            playDirect(at)
        else
            transcode(at)
        end if
    else if not m.transcodeRetried
        ' The server lost this conversion (a restart): start a new one here.
        m.transcodeRetried = true
        transcode(at)
    else
        showError("Playback failed: " + m.errorMsg)
    end if
end sub

' No answer at all, or the proxy in front saying the server is down.
function serverGone(res as object) as boolean
    code = getOr(res, "status", 0)
    return code = 0 or code = 502 or code = 503 or code = 504
end function

' The server doesn't answer: check again with growing gaps for up to two
' minutes, then carry on from the same spot the way it was playing.
sub lostServer(how as string, at as dynamic)
    now = CreateObject("roDateTime").AsSeconds()
    if m.reconnectUntil = 0
        m.reconnectUntil = now + 120
        m.reconnectTries = 0
    end if
    if now > m.reconnectUntil
        m.reconnectUntil = 0
        showError("Lost the server. Try again once it's back on.")
        return
    end if
    m.reconnectHow = how
    m.reconnectAt = at
    m.video.control = "stop"
    showBusy("Lost the server. Reconnecting...")
    delay = 2 * (2 ^ m.reconnectTries)
    if delay > 15 then delay = 15
    m.reconnectTries = m.reconnectTries + 1
    m.reconnectTimer.duration = delay
    m.reconnectTimer.control = "start"
end sub

sub onReconnectTimer()
    if m.top.closing then return
    api("GET", "/api/health", invalid, "onReconnectHealth", { timeout: 5000 })
end sub

sub onReconnectHealth(event as object)
    doneTask(event.getRoSGNode())
    if m.top.closing then return
    if not event.getData().ok
        lostServer(m.reconnectHow, m.reconnectAt)
        return
    end if
    m.transcodeRetried = false
    if m.reconnectHow = "direct" then playDirect(m.reconnectAt) else transcode(m.reconnectAt)
end sub

' The end, or Skip credits: report it watched and offer the next episode.
sub finish()
    if m.finished then return
    m.finished = true
    m.reportTimer.control = "stop"
    m.skip.visible = false
    report(true, true)
    if m.video.state <> "finished" then m.video.control = "stop"
    stopTranscode()
    findNext()
end sub

' Direct play stuck buffering: let the server convert from here.
sub onStall()
    if m.mode = "direct" and m.video.state = "buffering" then transcode(m.pos)
end sub

sub onReportTimer()
    ' Keep pinging while paused or buffering too, or the server drops this
    ' player from Now Playing after 45 s.
    s = m.video.state
    if s = "playing" or s = "paused" or s = "buffering" then report(false, false)
end sub

sub report(stopped as boolean, done as boolean, detached = false as boolean)
    if m.item = invalid or m.file = invalid then return
    p = m.pos
    if done then p = m.duration
    body = { playSessionId: m.playSession, itemId: m.item.id, fileId: m.file.id, position: p, duration: m.duration, paused: m.video.state = "paused", stopped: stopped, mode: m.mode, transcodeId: m.transcodeId, client: "Roku", played: Int(m.played) }
    if detached then apiSendDetached("POST", "/api/progress", body) else apiSend("POST", "/api/progress", body)
end sub

sub onClosing()
    if not m.top.closing then return
    m.reportTimer.control = "stop"
    m.nextTimer.control = "stop"
    m.togetherTimer.control = "stop"
    m.reconnectTimer.control = "stop"
    if not m.finished then report(true, false, true)
    m.video.control = "stop"
    if m.transcodeId <> ""
        apiSendDetached("DELETE", "/api/transcode/" + m.transcodeId, invalid)
        m.transcodeId = ""
    end if
end sub

' --- Subtitles ------------------------------------------------------------------

sub loadCues()
    m.cues = []
    m.cueIdx = 0
    m.cueLoad = m.cueLoad + 1
    drawCue()
    if not isTextSub(m.sub) then return
    api("GET", "/api/files/" + m.file.id.ToStr() + "/subtitles/" + m.sub.index.ToStr() + ".vtt", invalid, "onCues", { build: "vtt", load: m.cueLoad })
end sub

sub onCues(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    if t.request.load <> m.cueLoad then return ' another subtitle was picked meanwhile
    res = event.getData()
    if not res.ok
        say("Couldn't load these subtitles: " + res.error)
        return
    end if
    m.cues = res.data
    m.cueIdx = 0
    m.cueShown = -1
    drawCue()
end sub

' Shows the cue at the playhead (cues are sorted by start; cue times are file time).
sub drawCue()
    show = -1
    n = m.cues.Count()
    if n > 0 and isTextSub(m.sub)
        t = m.pos
        if m.seekTarget >= 0 then t = -1
        if m.cueIdx >= n or (m.cueIdx > 0 and t < m.cues[m.cueIdx].s) then m.cueIdx = 0
        while m.cueIdx < n and m.cues[m.cueIdx].e <= t
            m.cueIdx = m.cueIdx + 1
        end while
        if m.cueIdx < n and m.cues[m.cueIdx].s <= t then show = m.cueIdx
    end if
    raised = m.overlay.visible
    if show = m.cueShown and raised = m.subsRaised then return
    m.cueShown = show
    m.subsRaised = raised
    lines = []
    if show >= 0
        for each l in m.cues[show].lines
            lines.Append(wrapLine(l, 46))
        end for
    end if
    while lines.Count() > 3
        lines.Shift()
    end while
    size = subtitleSize()
    lineH = Int(size * 1.35)
    bottom = 1010
    if raised then bottom = 830
    top = bottom - lines.Count() * lineH
    bgColor = "0x00000070"
    if isTrue(getOr(m.prefs, "subtitleBackground", false)) then bgColor = "0x000000D0"
    for i = 0 to 2
        label = m.top.findNode("subLine" + i.ToStr())
        bg = m.top.findNode("subBg" + i.ToStr())
        if i < lines.Count()
            label.font.size = size
            label.text = lines[i]
            r = label.boundingRect()
            w = r.width
            if w <= 0 or w > 1800 then w = 1700
            x = (1920 - w) / 2
            y = top + i * lineH
            label.translation = [x, y + (lineH - size * 1.2) / 2]
            label.visible = true
            bg.width = w + 40
            bg.height = lineH
            bg.translation = [x - 20, y]
            bg.color = bgColor
            bg.visible = true
        else
            label.text = ""
            label.visible = false
            bg.visible = false
        end if
    end for
end sub

function subtitleSize() as integer
    s = getOr(m.prefs, "subtitleSize", "medium")
    if s = "small" then return 38
    if s = "large" then return 60
    return 48
end function

' Long lines broken at spaces so they fit across the screen.
function wrapLine(text as string, width as integer) as object
    out = []
    rest = text
    while Len(rest) > width
        cut = 0
        for i = width to 1 step -1
            if Mid(rest, i, 1) = " "
                cut = i
                exit for
            end if
        end for
        if cut = 0 then cut = width
        out.Push(Left(rest, cut).Trim())
        rest = Mid(rest, cut + 1).Trim()
    end while
    if rest <> "" then out.Push(rest)
    return out
end function

' --- Intro and credits ------------------------------------------------------------

function markerOf(kind as string) as dynamic
    for each mk in m.markers
        if getOr(mk, "kind", "") = kind then return mk
    end for
    return invalid
end function

' A movie with a scene after the credits jumps to that scene instead of ending.
function afterCredits() as boolean
    c = markerOf("credits")
    return m.item <> invalid and m.item.kind <> "episode" and c <> invalid and c["end"] < m.duration - 5
end function

' Offers Skip intro / Skip credits, or skips by itself, as the viewer's
' preferences say (once per visit, so seeking back into the intro is fine).
sub checkMarkers()
    if m.mode = "trailer" or m.finished or m.seekTarget >= 0 then return
    t = m.pos
    kind = ""
    intro = markerOf("intro")
    if intro <> invalid and not m.introDone and t >= intro.start and t < intro["end"] - 1
        mode = getOr(m.prefs, "skipIntro", "button")
        if mode = "auto"
            m.introDone = true
            say("Skipped the intro")
            userSeek(intro["end"])
        else if mode = "button"
            kind = "intro"
        end if
    end if
    credits = markerOf("credits")
    if credits <> invalid and not m.creditsDone and not m.upNext.visible
        last = credits["end"]
        if m.duration > 0 and last > m.duration then last = m.duration
        if t >= credits.start and t < last - 1
            mode = getOr(m.prefs, "skipCredits", "button")
            if mode = "auto" and (m.item.kind = "episode" or afterCredits())
                if afterCredits() then say("Skipped to the scene after the credits")
                skipCredits()
                return
            else if mode = "button"
                kind = "credits"
            end if
        end if
    end if
    if kind = m.skipKind then return
    m.skipKind = kind
    m.skip.visible = kind <> ""
    if kind = "intro"
        m.top.findNode("skipLabel").text = "Skip intro"
    else if kind = "credits"
        if m.item.kind = "episode" then m.top.findNode("skipLabel").text = "Next episode" else m.top.findNode("skipLabel").text = "Skip credits"
    end if
end sub

sub doSkip()
    kind = m.skipKind
    m.skipKind = ""
    m.skip.visible = false
    if kind = "intro"
        m.introDone = true
        intro = markerOf("intro")
        if intro <> invalid then userSeek(intro["end"])
    else if kind = "credits"
        skipCredits()
    end if
end sub

sub skipCredits()
    m.creditsDone = true
    m.skip.visible = false
    m.skipKind = ""
    c = markerOf("credits")
    if afterCredits() and c <> invalid then userSeek(c["end"]) else finish()
end sub

' --- Controls -----------------------------------------------------------------

sub showOverlay()
    m.overlay.visible = true
    if m.mode = "direct"
        mode = "Direct play"
    else if m.mode = "trailer"
        mode = "Trailer"
    else
        mode = "Converting"
        if m.quality > 0 then mode = mode + " at " + resolutionLabel(m.quality)
    end if
    if m.video.state = "paused" then mode = "Paused  ·  " + mode
    if m.room <> "" then mode = mode + "  ·  Together (" + roomCount().ToStr() + ")"
    m.top.findNode("state").text = mode
    if m.mode = "trailer"
        hint = ""
    else if m.chapters.Count() > 0
        hint = "* Options: chapters, audio, subtitles, quality, watch together"
    else
        hint = "* Options: audio, subtitles, quality, watch together"
    end if
    m.top.findNode("hint").text = hint
    drawProgress()
    drawCue()
    m.hideTimer.control = "stop"
    if m.video.state = "playing" then m.hideTimer.control = "start"
end sub

sub hideOverlay()
    if m.video.state = "paused" or m.seekTarget >= 0 then return
    m.overlay.visible = false
    drawCue()
end sub

sub drawProgress()
    t = m.pos
    if m.seekTarget >= 0 then t = m.seekTarget
    d = m.duration
    frac = 0.0
    if d > 0 then frac = t / d
    if frac > 1 then frac = 1
    if frac < 0 then frac = 0
    m.top.findNode("bar").width = 1700 * frac
    m.top.findNode("handle").translation = [110 + 1700 * frac - 3, 929]
    m.top.findNode("elapsed").text = clockTime(t)
    if d > 0 then m.top.findNode("total").text = "-" + clockTime(d - t) else m.top.findNode("total").text = ""
    if m.mode = "trailer" then m.top.findNode("chapter").text = "" else m.top.findNode("chapter").text = chapterName(chapterAt(t))
end sub

' Presses add up; the jump happens once they stop.
sub seekBy(delta as float)
    if m.seekTarget < 0 then m.seekTarget = m.pos
    m.seekTarget = m.seekTarget + delta
    if m.seekTarget < 0 then m.seekTarget = 0
    if m.duration > 10 and m.seekTarget > m.duration - 5 then m.seekTarget = m.duration - 5
    showOverlay()
    m.seekTimer.control = "stop"
    m.seekTimer.control = "start"
end sub

sub commitSeek()
    t = m.seekTarget
    m.seekTarget = -1
    if t < 0 then return
    userSeek(t)
    showOverlay()
end sub

sub seekTo(t as float)
    if m.mode <> "transcode"
        m.video.seek = t
    else if t >= m.offset and t < m.offset + m.video.duration - 8
        ' Already converted: seek within the playlist.
        m.video.seek = t - m.offset
    else
        m.pos = t
        transcode(t)
    end if
    m.cueIdx = 0
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if m.upNext.visible
        if key = "OK" or key = "play"
            playNext()
            return true
        end if
        if key = "back"
            m.nextTimer.control = "stop"
            return false
        end if
        return true
    end if
    if key = "OK" and m.skip.visible
        doSkip()
        return true
    end if
    if key = "OK" or key = "play"
        if m.video.state = "paused"
            m.video.control = "resume"
            sendRoom(false, m.pos)
        else if m.video.state = "playing"
            m.video.control = "pause"
            sendRoom(true, m.pos)
        end if
        showOverlay()
        return true
    else if key = "left"
        seekBy(-10)
        return true
    else if key = "right"
        seekBy(30)
        return true
    else if key = "rewind"
        seekBy(-60)
        return true
    else if key = "fastforward"
        seekBy(60)
        return true
    else if key = "replay"
        seekBy(-10)
        return true
    else if key = "up" or key = "down"
        showOverlay()
        return true
    else if key = "options"
        if m.file <> invalid and m.mode <> "trailer" then openMenu("main")
        return true
    else if key = "back" and m.seekTarget >= 0
        m.seekTarget = -1
        m.seekTimer.control = "stop"
        showOverlay()
        return true
    end if
    return false
end function

' --- Chapters ----------------------------------------------------------------
' Marks on the progress bar, the chapter's name under it, and a Chapters list
' in Options. Times are seconds in the original file, like m.pos.

sub onChapters(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok or type(res.data) <> "roAssociativeArray" then return
    c = getOr(res.data, "chapters", [])
    if type(c) <> "roArray" then return
    m.chapters = c
    drawTicks()
    if m.overlay.visible then showOverlay()
end sub

sub drawTicks()
    ticks = m.top.findNode("ticks")
    ticks.removeChildrenIndex(ticks.getChildCount(), 0)
    d = m.duration
    if d <= 0 or m.chapters.Count() < 2 then return
    for i = 1 to m.chapters.Count() - 1
        st = getOr(m.chapters[i], "start", 0)
        if st > 0 and st < d
            r = ticks.createChild("Rectangle")
            r.width = 4
            r.height = 8
            r.color = "0x000000C0"
            r.translation = [110 + 1700 * (st / d) - 2, 940]
        end if
    end for
end sub

function chapterAt(t as float) as integer
    for i = m.chapters.Count() - 1 to 0 step -1
        if t >= getOr(m.chapters[i], "start", 0) then return i
    end for
    return -1
end function

function chapterName(i as integer) as string
    if i < 0 or i >= m.chapters.Count() then return ""
    t = getOr(m.chapters[i], "title", "")
    if t = "" then return "Chapter " + (i + 1).ToStr()
    return t
end function

sub chooseChapter(i as integer)
    st = getOr(m.chapters[i], "start", 0)
    m.seekTarget = -1
    m.seekTimer.control = "stop"
    userSeek(st)
    say(chapterName(i))
    showOverlay()
end sub

' --- Options: audio, subtitles and quality -------------------------------------------

function resolutionLabel(h as integer) as string
    if h >= 2160 then return "4K"
    return h.ToStr() + "p"
end function

function qualityLabel() as string
    if m.mode = "direct" then return "Direct play"
    if m.quality > 0 then return "Convert at " + resolutionLabel(m.quality)
    return "Convert"
end function

sub openMenu(level as string)
    m.menuLevel = level
    labels = []
    m.menuActions = []
    focus = 0
    audios = streamsOf(m.file, "audio")
    subs = streamsOf(m.file, "subtitle")
    if level = "main"
        m.menu.title = m.top.findNode("title").text
        m.menu.message = ""
        if m.chapters.Count() > 0
            labels.Push("Chapters: " + chapterName(chapterAt(m.pos)))
            m.menuActions.Push({ act: "level", level: "chapters" })
        end if
        if audios.Count() > 1
            a = audioFor(m.file, m.audio)
            labels.Push("Audio: " + streamLabel(a))
            m.menuActions.Push({ act: "level", level: "audio" })
        end if
        if m.sub <> invalid then s = streamLabel(m.sub) else s = "Off"
        labels.Push("Subtitles: " + s)
        m.menuActions.Push({ act: "level", level: "subs" })
        labels.Push("Quality: " + qualityLabel())
        m.menuActions.Push({ act: "level", level: "quality" })
        if m.room <> "" then labels.Push("Together (" + roomCount().ToStr() + "): invite or leave") else labels.Push("Watch together")
        m.menuActions.Push({ act: "level", level: "together" })
        if m.item <> invalid and m.top.params.trailer = invalid and (m.item.kind = "movie" or m.item.kind = "episode")
            labels.Push("Report a problem")
            m.menuActions.Push({ act: "level", level: "problem" })
        end if
    else if level = "problem"
        m.menu.title = "Report a problem"
        m.menu.message = "An admin sees it with where you are now."
        kinds = [["video", "Picture"], ["audio", "Sound"], ["subtitles", "Subtitles"], ["wrong", "Wrong file"], ["other", "Something else"]]
        for each k in kinds
            labels.Push(k[1])
            m.menuActions.Push({ act: "problem", kind: k[0] })
        end for
    else if level = "chapters"
        m.menu.title = "Chapters"
        m.menu.message = ""
        focus = chapterAt(m.pos)
        for i = 0 to m.chapters.Count() - 1
            labels.Push(clockTime(getOr(m.chapters[i], "start", 0)) + "   " + chapterName(i))
            m.menuActions.Push({ act: "chapter", index: i })
        end for
    else if level = "audio"
        m.menu.title = "Audio"
        cur = audioFor(m.file, m.audio)
        def = fileDefaultAudio(m.file)
        for each a in audios
            if cur <> invalid and a.index = cur.index then focus = labels.Count()
            labels.Push(streamLabel(a))
            idx = a.index
            if def <> invalid and a.index = def.index then idx = -1
            m.menuActions.Push({ act: "audio", index: idx })
        end for
    else if level = "subs"
        m.menu.title = "Subtitles"
        if subs.Count() = 0 then m.menu.message = "This file has no subtitles." else m.menu.message = ""
        labels.Push("Off")
        m.menuActions.Push({ act: "sub", stream: invalid })
        for each s in subs
            if m.sub <> invalid and s.index = m.sub.index then focus = labels.Count()
            l = streamLabel(s)
            if isImageSub(s) then l = l + " · burned in"
            labels.Push(l)
            m.menuActions.Push({ act: "sub", stream: s })
        end for
    else if level = "together"
        togetherMenu(labels)
    else if level = "quality"
        m.menu.title = "Quality"
        m.menu.message = "Converting takes a moment to start. A lower quality helps when playback keeps stopping."
        if m.burn < 0 and canDirectPlay(m.file)
            labels.Push("Direct play (original)")
            m.menuActions.Push({ act: "quality", q: -1 })
        end if
        if m.quality = 0 or (m.quality < 0 and m.mode = "transcode") then focus = labels.Count()
        labels.Push("Convert")
        m.menuActions.Push({ act: "quality", q: 0 })
        for each h in [2160, 1080, 720, 480]
            if h < getOr(m.file, "height", 0)
                if m.quality = h then focus = labels.Count()
                labels.Push("Convert at " + resolutionLabel(h))
                m.menuActions.Push({ act: "quality", q: h })
            end if
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
    if a.act = "level"
        if a.level = "together" then m.roomPeople = invalid
        openMenu(a.level)
        return
    end if
    if a.act = "startRoom"
        startRoom()
        return
    end if
    if a.act = "invite"
        invite(a.person)
        return
    end if
    closeMenu()
    if a.act = "leaveRoom"
        leaveRoom("You left the watch party.")
        return
    end if
    if a.act = "audio"
        chooseAudio(a.index)
    else if a.act = "sub"
        chooseSub(a.stream)
    else if a.act = "quality"
        chooseQuality(a.q)
    else if a.act = "chapter"
        chooseChapter(a.index)
    else if a.act = "problem"
        api("POST", "/api/items/" + m.item.id.ToStr() + "/problems", { kind: a.kind, position: Int(m.pos) }, "onProblemSent")
    end if
end sub

sub onProblemSent(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok then say("Thanks, an admin will take a look") else say(res.error)
end sub

' Back in a list goes up to the first one, then closes.
sub onMenuClosed()
    if m.menuLevel <> "main" then openMenu("main") else closeMenu()
end sub

sub closeMenu()
    m.menu.visible = false
    m.menuLevel = ""
    ' A child, not the screen itself: focusing an ancestor of the focused
    ' menu can leave the menu holding the keys.
    m.top.findNode("keys").setFocus(true)
end sub

sub chooseAudio(index as integer)
    if index = m.audio then return
    m.audio = index
    if m.mode = "transcode"
        transcode(m.pos) ' the server mixes the new track
    else if not canDirectPlay(m.file) or m.quality >= 0
        transcode(m.pos) ' no decoder for that track here
    else
        applyDirectAudio()
    end if
end sub

sub chooseSub(s as dynamic)
    m.sub = s
    burn = -1
    if isImageSub(s) then burn = s.index
    if burn <> m.burn
        m.burn = burn
        if burn < 0 and m.quality < 0 and canDirectPlay(m.file) then playDirect(m.pos) else transcode(m.pos)
    end if
    m.cueShown = -2
    loadCues()
end sub

sub chooseQuality(q as integer)
    m.quality = q
    if q < 0 and canDirectPlay(m.file) then playDirect(m.pos) else transcode(m.pos)
end sub

' --- Up next ----------------------------------------------------------------

sub findNext()
    if m.item = invalid or m.item.kind <> "episode"
        m.top.navigate = { action: "back" }
        return
    end if
    api("GET", "/api/items/" + m.item.id.ToStr() + "/next", invalid, "onNext")
end sub

sub onNext(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok or type(res.data) <> "roAssociativeArray" or res.data.id = invalid
        m.top.navigate = { action: "back" }
        return
    end if
    m.next = res.data
    m.top.findNode("nextTitle").text = joinStrings([episodeCode(m.next), getOr(m.next, "title", "")], " · ")
    m.upNext.visible = true
    ' The viewer turned the countdown off: wait for OK.
    if m.prefs <> invalid and getOr(m.prefs, "autoPlayNext", true) = false
        m.top.findNode("nextCount").text = ""
        return
    end if
    m.countdown = 10
    m.top.findNode("nextCount").text = "Playing in 10"
    m.nextTimer.control = "start"
end sub

sub onNextTick()
    m.countdown = m.countdown - 1
    if m.countdown <= 0
        playNext()
    else
        m.top.findNode("nextCount").text = "Playing in " + m.countdown.ToStr()
    end if
end sub

sub playNext()
    m.nextTimer.control = "stop"
    params = { id: m.next.id, start: 0 }
    if m.room <> ""
        ' Everyone in the watch party moves on with this viewer (the next
        ' player tells the room as it opens).
        params.together = m.room
        params.announce = true
    end if
    m.top.navigate = { action: "replace", screen: "PlayerScreen", params: params }
end sub

' --- Watch Together ---------------------------------------------------------
'
' The same rooms as the web and the Android TV app. Roku has no server-sent
' events, so the room is read every second and a half (which also keeps this
' viewer counted as in it). Someone else's play, pause, seek or next episode
' is applied here; this viewer's own go out through sendRoom.

' The time now in ms since 1970, as a Double (too big for an Integer).
function nowMs() as double
    d = CreateObject("roDateTime")
    return CDbl(d.AsSeconds()) * 1000# + d.GetMilliseconds()
end function

' Where the room's playback is right now, in seconds.
function roomPosition(st as object) as float
    p = getOr(st, "position", 0)
    if isTrue(st.paused) then return p
    ahead = (nowMs() + m.roomSkew - CDbl(getOr(st, "at", 0))) / 1000#
    if ahead < 0 then ahead = 0
    p = p + ahead
    ' A clock far off can't send the player past the end.
    if m.duration > 5 and p > m.duration - 5 then p = m.duration - 5
    return p
end function

' Notes the server's clock from an answer to a request sent at m.roomAsked
' (kept here: a Double in a task's request can lose its precision): half the
' round trip is how old the server's reading is.
sub roomClock(res as object)
    if res.data = invalid or res.data.now = invalid or m.roomAsked <= 0 then return
    m.roomSkew = CDbl(res.data.now) - (m.roomAsked + nowMs()) / 2#
end sub

' Joining: where the room is says where to start and whether to wait paused.
sub onRoomFirst(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    if m.top.closing then return
    res = event.getData()
    if res.ok and type(res.data) = "roAssociativeArray" and res.data.state <> invalid
        roomClock(res)
        st = res.data.state
        m.roomRev = getOr(st, "rev", 0)
        m.roomMembers = getOr(res.data, "members", [])
        if getOr(st, "itemId", 0) = m.top.params.id
            m.roomStart = roomPosition(st)
            m.roomPaused = isTrue(st.paused)
        else
            ' The party already moved on to something else: go there.
            m.waiting = -1
            m.top.navigate = { action: "replace", screen: "PlayerScreen", params: { id: st.itemId, start: roomPosition(st), together: m.room } }
            return
        end if
    else
        m.room = ""
        say("That watch party has ended.")
    end if
    m.waiting = m.waiting - 1
    if m.waiting = 0 then start()
end sub

sub pollRoom()
    if m.room = "" or m.roomPolling then return
    m.roomPolling = true
    api("GET", "/api/together/" + m.room + "?watching=1", invalid, "onRoom", { room: m.room })
    m.roomAsked = nowMs()
end sub

sub onRoom(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    m.roomPolling = false
    if m.top.closing or t.request.room <> m.room then return
    res = event.getData()
    if not res.ok
        if res.status = 404 or res.status = 403 then leaveRoom("The watch party has ended.")
        return
    end if
    if type(res.data) <> "roAssociativeArray" or res.data.state = invalid then return
    roomClock(res)
    m.roomMembers = getOr(res.data, "members", [])
    st = res.data.state
    if getOr(st, "rev", 0) > m.roomRev
        m.roomRev = st.rev
        applyRoom(st)
    end if
end sub

' Someone else changed the room: go where it is now.
sub applyRoom(st as object)
    want = roomPosition(st)
    if m.item <> invalid and getOr(st, "itemId", 0) <> m.item.id
        m.top.navigate = { action: "replace", screen: "PlayerScreen", params: { id: st.itemId, start: want, together: m.room } }
        return
    end if
    jumped = Abs(want - m.pos) > 2
    if jumped then seekTo(want)
    paused = isTrue(st.paused)
    s = m.video.state
    wasPaused = s = "paused"
    if s = "playing" or s = "paused"
        if paused and s = "playing" then m.video.control = "pause"
        if not paused and s = "paused" then m.video.control = "resume"
    else
        ' Still starting or converting: settle once it plays.
        m.pendingPause = paused
    end if
    me = getOr(m.global.user, "id", 0)
    who = getOr(st, "byName", "")
    if getOr(st, "by", 0) <> me and who <> ""
        if paused <> wasPaused
            if paused then say(who + " paused") else say(who + " pressed play")
        else if jumped
            say(who + " jumped to " + clockTime(want))
        end if
    end if
    if m.overlay.visible then showOverlay()
end sub

' This viewer played, paused or jumped: tell the room.
sub sendRoom(paused as boolean, at as float)
    if m.room = "" then return
    api("POST", "/api/together/" + m.room + "/state", { paused: paused, position: at }, "onRoomSent", { room: m.room })
end sub

sub onRoomSent(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok or t.request.room <> m.room or type(res.data) <> "roAssociativeArray" or res.data.state = invalid then return
    ' Our own change: don't apply it again when the next look sees it.
    if getOr(res.data.state, "rev", 0) > m.roomRev then m.roomRev = res.data.state.rev
    m.roomMembers = getOr(res.data, "members", m.roomMembers)
end sub

' A seek the viewer asked for (the remote, Skip intro), which the room follows.
sub userSeek(t as float)
    seekTo(t)
    sendRoom(m.video.state = "paused", t)
end sub

function roomCount() as integer
    n = m.roomMembers.Count()
    if n < 1 then n = 1
    return n
end function

' Options -> Watch together: start a room, or invite people and leave.
sub togetherMenu(labels as object)
    m.menu.title = "Watch together"
    if m.room = ""
        m.menu.message = "Watch with someone else on this server, each on your own screen. Play, pause and skipping stay in step."
        labels.Push("Start watching together")
        m.menuActions.Push({ act: "startRoom" })
        return
    end if
    names = []
    for each mem in m.roomMembers
        names.Push(getOr(mem, "name", ""))
    end for
    who = joinStrings(names, ", ")
    if who = "" then who = "just you so far"
    m.menu.message = "Watching with: " + who + ". Invite someone and it pops up on their TV, phone or browser."
    if m.roomPeople = invalid
        m.roomLoad = m.roomLoad + 1
        api("GET", "/api/together/" + m.room + "/people", invalid, "onRoomPeople", { load: m.roomLoad })
    else
        inRoom = {}
        for each mem in m.roomMembers
            inRoom[asString(mem.userId)] = true
        end for
        for each p in m.roomPeople
            if not inRoom.DoesExist(asString(p.id))
                if m.roomInvited.DoesExist(asString(p.id)) then labels.Push(getOr(p, "name", "") + ": invited") else labels.Push("Invite " + getOr(p, "name", ""))
                m.menuActions.Push({ act: "invite", person: p })
            end if
        end for
        if m.roomPeople.Count() = 0 then m.menu.message = m.menu.message + " Nobody else can play this title here."
    end if
    labels.Push("Leave the watch party")
    m.menuActions.Push({ act: "leaveRoom" })
end sub

sub onRoomPeople(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    if t.request.load <> m.roomLoad then return
    res = event.getData()
    if res.ok and type(res.data) = "roArray" then m.roomPeople = res.data else m.roomPeople = []
    if m.menu.visible and m.menuLevel = "together" then openMenu("together")
end sub

sub startRoom()
    if m.item = invalid then return
    api("POST", "/api/together", { itemId: m.item.id, position: m.pos, paused: m.video.state = "paused" }, "onRoomStarted")
    m.roomAsked = nowMs()
end sub

sub onRoomStarted(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok or type(res.data) <> "roAssociativeArray"
        m.menu.message = "Couldn't start it: " + asString(res.error)
        return
    end if
    roomClock(res)
    m.room = res.data.id
    m.roomRev = getOr(res.data.state, "rev", 0)
    m.roomMembers = getOr(res.data, "members", [])
    m.roomPeople = invalid
    m.roomInvited = {}
    m.togetherTimer.control = "start"
    pollRoom()
    if m.menu.visible then openMenu("together")
end sub

sub invite(p as object)
    api("POST", "/api/together/" + m.room + "/invite", { userIds: [p.id] }, "onInvited", { name: getOr(p, "name", ""), id: p.id })
end sub

sub onInvited(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        m.menu.message = res.error
        return
    end if
    m.roomInvited[asString(t.request.id)] = true
    if m.menu.visible and m.menuLevel = "together"
        f = m.menu.chosen
        openMenu("together")
        m.menu.message = t.request.name + " is invited. It pops up on their screen."
        m.menu.focus = f
    end if
end sub

sub leaveRoom(message as string)
    if m.room = "" then return
    m.room = ""
    m.roomMembers = []
    m.roomPeople = invalid
    m.roomInvited = {}
    m.pendingPause = invalid
    m.togetherTimer.control = "stop"
    say(message)
    if m.overlay.visible then showOverlay()
end sub

' --- Status -----------------------------------------------------------------

sub say(text as string)
    m.toast.text = text
    m.toastTimer.control = "stop"
    m.toastTimer.control = "start"
end sub

sub hideToast()
    m.toast.text = ""
end sub

sub showBusy(text as string)
    m.spinner.visible = true
    m.spinner.control = "start"
    m.status.text = text
end sub

sub hideBusy()
    m.spinner.visible = false
    m.spinner.control = "stop"
    m.status.text = ""
end sub

sub showError(text as string)
    hideBusy()
    m.status.text = text + Chr(10) + "Press Back to return."
end sub
