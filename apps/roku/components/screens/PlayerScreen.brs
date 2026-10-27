' The player. Direct play when roDeviceInfo says this Roku decodes the file's
' container, video and audio; otherwise (or when direct play fails or
' stalls) the server converts it to HLS from the current position. The
' controls are our own so seeking can restart a conversion at the new point.

sub init()
    m.video = m.top.findNode("video")
    m.overlay = m.top.findNode("overlay")
    m.status = m.top.findNode("status")
    m.spinner = m.top.findNode("spinner")
    m.spinner.poster.uri = "pkg:/images/spinner.png"
    m.upNext = m.top.findNode("upNext")
    m.reportTimer = m.top.findNode("reportTimer")
    m.hideTimer = m.top.findNode("hideTimer")
    m.seekTimer = m.top.findNode("seekTimer")
    m.stallTimer = m.top.findNode("stallTimer")
    m.nextTimer = m.top.findNode("nextTimer")
    m.reportTimer.observeField("fire", "onReportTimer")
    m.hideTimer.observeField("fire", "hideOverlay")
    m.seekTimer.observeField("fire", "commitSeek")
    m.stallTimer.observeField("fire", "onStall")
    m.nextTimer.observeField("fire", "onNextTick")
    m.video.observeField("state", "onState")
    m.video.observeField("position", "onPosition")
    m.top.observeField("params", "onParams")
    m.top.observeField("closing", "onClosing")
    m.top.observeField("focusedChild", "onFocusChanged")

    m.item = invalid
    m.file = invalid
    m.mode = ""
    m.offset = 0.0 ' a conversion starts this many seconds into the file
    m.transcodeId = ""
    m.pos = 0.0
    m.played = 0.0
    m.duration = 0.0
    m.seekTarget = -1.0
    m.finished = false
    m.checkStart = false
    m.wasPaused = false
    m.next = invalid
    m.playSession = "roku-" + CreateObject("roDeviceInfo").GetRandomUUID()
    m.di = CreateObject("roDeviceInfo")
    showBusy("Loading...")
end sub

sub onFocusChanged()
    ' Keys come to this screen; the Video node never takes focus.
end sub

sub onParams()
    api("GET", "/api/items/" + m.top.params.id.ToStr(), invalid, "onItem")
end sub

sub onItem(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        showError(res.error)
        return
    end if
    m.item = res.data.item
    files = getOr(res.data, "files", [])
    if files.Count() = 0
        showError("This item has no playable file.")
        return
    end if
    m.file = files[0]
    m.duration = getOr(m.file, "duration", 0)
    if m.duration <= 0 then m.duration = getOr(m.item, "runtime", 0) * 60
    it = m.item
    if it.kind = "episode"
        m.top.findNode("title").text = getOr(it, "showTitle", it.title)
        m.top.findNode("subtitle").text = joinStrings([episodeCode(it), it.title], " · ")
    else
        m.top.findNode("title").text = it.title
        if it.year <> invalid then m.top.findNode("subtitle").text = it.year.ToStr()
    end if
    start = getOr(m.top.params, "start", 0)
    m.pos = start
    if canDirectPlay(m.file) then playDirect(start) else transcode(start)
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

function defaultAudio(f as object) as dynamic
    first = invalid
    for each s in getOr(f, "streams", [])
        if s.type = "audio"
            if isTrue(s.default) then return s
            if first = invalid then first = s
        end if
    end for
    return first
end function

function canDirectPlay(f as object) as boolean
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
    a = defaultAudio(f)
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
    c.PlayStart = at
    showBusy("")
    m.video.content = c
    m.video.control = "play"
end sub

sub transcode(at as dynamic)
    m.mode = "transcode"
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
    body = { start: at, maxHeight: maxHeight, audioStream: -1, burnSubtitle: -1, replace: m.transcodeId, adaptive: true, encodeVideo: false, audioCodecs: codecs, maxAudioChannels: channels }
    m.pendingAt = at
    api("POST", "/api/files/" + m.file.id.ToStr() + "/transcode", body, "onTranscode", { timeout: 30000 })
end sub

sub onTranscode(event as object)
    doneTask(event.getRoSGNode())
    if m.top.closing then return
    res = event.getData()
    if not res.ok
        showError(res.error)
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
    c.PlayStart = 0
    m.video.content = c
    m.video.control = "play"
end sub

sub stopTranscode()
    if m.transcodeId <> ""
        apiSend("DELETE", "/api/transcode/" + m.transcodeId, invalid)
        m.transcodeId = ""
    end if
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
    if m.overlay.visible then drawProgress()
end sub

sub onState()
    s = m.video.state
    if s = "buffering"
        showBusy("")
        m.stallTimer.control = "start"
    else if s = "playing"
        m.stallTimer.control = "stop"
        hideBusy()
        ' Some firmware joins a growing playlist near its end like a live
        ' stream; go back to where the conversion started.
        if m.checkStart
            m.checkStart = false
            if m.video.position > 20 then m.video.seek = 0
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
        m.finished = true
        m.reportTimer.control = "stop"
        report(true, true)
        stopTranscode()
        findNext()
    else if s = "error"
        m.stallTimer.control = "stop"
        if m.mode = "direct"
            ' A profile or level this Roku can't decode after all: convert.
            transcode(m.pos)
        else
            showError("Playback failed: " + m.video.errorMsg)
        end if
    end if
end sub

' Direct play stuck buffering: let the server convert from here.
sub onStall()
    if m.mode = "direct" and m.video.state = "buffering" then transcode(m.pos)
end sub

sub onReportTimer()
    if m.video.state = "playing" then report(false, false)
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
    if not m.finished then report(true, false, true)
    m.video.control = "stop"
    if m.transcodeId <> ""
        apiSendDetached("DELETE", "/api/transcode/" + m.transcodeId, invalid)
        m.transcodeId = ""
    end if
end sub

' --- Controls -----------------------------------------------------------------

sub showOverlay()
    m.overlay.visible = true
    if m.mode = "direct" then mode = "Direct play" else mode = "Converting"
    if m.video.state = "paused" then mode = "Paused  ·  " + mode
    m.top.findNode("state").text = mode
    if m.mode = "direct" then hint = "* Options: convert instead" else hint = ""
    if m.mode = "transcode" and m.file <> invalid and canDirectPlay(m.file) then hint = "* Options: direct play"
    m.top.findNode("hint").text = hint
    drawProgress()
    m.hideTimer.control = "stop"
    if m.video.state = "playing" then m.hideTimer.control = "start"
end sub

sub hideOverlay()
    if m.video.state = "paused" or m.seekTarget >= 0 then return
    m.overlay.visible = false
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
    if m.mode = "direct"
        m.video.seek = t
    else if t >= m.offset and t < m.offset + m.video.duration - 8
        ' Already converted: seek within the playlist.
        m.video.seek = t - m.offset
    else
        m.pos = t
        transcode(t)
    end if
    showOverlay()
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
    if key = "OK" or key = "play"
        if m.video.state = "paused"
            m.video.control = "resume"
        else if m.video.state = "playing"
            m.video.control = "pause"
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
        if m.file = invalid then return true
        if m.mode = "direct"
            transcode(m.pos)
        else if canDirectPlay(m.file)
            playDirect(m.pos)
        end if
        return true
    else if key = "back" and m.seekTarget >= 0
        m.seekTarget = -1
        m.seekTimer.control = "stop"
        showOverlay()
        return true
    end if
    return false
end function

' --- Up next ----------------------------------------------------------------

sub findNext()
    if m.item.kind <> "episode"
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
    m.countdown = 10
    m.top.findNode("nextCount").text = "Playing in 10"
    m.upNext.visible = true
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
    m.top.navigate = { action: "replace", screen: "PlayerScreen", params: { id: m.next.id, start: 0 } }
end sub

' --- Status -----------------------------------------------------------------

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
