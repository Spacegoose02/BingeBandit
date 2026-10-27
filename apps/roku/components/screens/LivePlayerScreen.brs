' Live TV: joins the channel's shared stream on the server (which answers
' once the first segments exist), plays its HLS, and leaves on the way out.

sub init()
    m.video = m.top.findNode("video")
    m.overlay = m.top.findNode("overlay")
    m.status = m.top.findNode("status")
    m.spinner = m.top.findNode("spinner")
    m.spinner.poster.uri = "pkg:/images/spinner.png"
    m.hideTimer = m.top.findNode("hideTimer")
    m.tuneTimer = m.top.findNode("tuneTimer")
    m.bufferTimer = m.top.findNode("bufferTimer")
    m.pingTimer = m.top.findNode("pingTimer")
    m.hideTimer.observeField("fire", "hideOverlay")
    m.tuneTimer.observeField("fire", "tune")
    m.bufferTimer.observeField("fire", "onBuffering")
    m.pingTimer.observeField("fire", "onPing")
    m.video.observeField("state", "onState")
    m.top.observeField("params", "onParams")
    m.top.observeField("closing", "onClosing")
    m.viewerId = ""
    m.joinCount = 0
    m.bufferedFor = 0
    m.failed = false
end sub

sub onParams()
    m.channels = m.top.params.channels
    m.index = m.top.params.index
    showChannel()
    tune()
end sub

sub showChannel()
    c = m.channels[m.index]
    m.top.findNode("channel").text = asString(c.number) + "   " + getOr(c, "name", "")
    logo = ""
    if getOr(c, "logo", "") <> "" then logo = withToken(absoluteUrl(c.logo))
    m.top.findNode("logo").uri = logo
    now = getOr(c, "now", invalid)
    nxt = getOr(c, "next", invalid)
    if now <> invalid
        m.top.findNode("now").text = wallClock(now.start) + " - " + wallClock(now.stop) + "   " + getOr(now, "title", "")
    else
        m.top.findNode("now").text = ""
    end if
    if nxt <> invalid then m.top.findNode("next").text = "Next  " + wallClock(nxt.start) + "   " + getOr(nxt, "title", "") else m.top.findNode("next").text = ""
    m.overlay.visible = true
    m.hideTimer.control = "stop"
    m.hideTimer.control = "start"
end sub

sub hideOverlay()
    if m.video.state <> "paused" then m.overlay.visible = false
end sub

sub leave(detached as boolean)
    if m.viewerId = "" then return
    path = "/api/live/viewers/" + m.viewerId
    m.viewerId = ""
    if detached then apiSendDetached("DELETE", path, invalid) else apiSend("DELETE", path, invalid)
end sub

sub tune()
    leave(false)
    m.video.control = "stop"
    m.bufferTimer.control = "stop"
    showBusy("Tuning...")
    m.joinCount = m.joinCount + 1
    c = m.channels[m.index]
    ' The server waits for the stream's first segments, up to ~25 s.
    api("POST", "/api/live/channels/" + c.id.ToStr() + "/watch", {}, "onJoined", { timeout: 40000, join: m.joinCount })
end sub

sub onJoined(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if m.top.closing
        if res.ok then apiSendDetached("DELETE", "/api/live/viewers/" + res.data.viewerId, invalid)
        return
    end if
    if t.request.join <> m.joinCount
        ' Flipped past this channel while it was joining.
        if res.ok then apiSend("DELETE", "/api/live/viewers/" + res.data.viewerId, invalid)
        return
    end if
    if not res.ok
        hideBusy()
        m.status.text = res.error
        return
    end if
    m.viewerId = res.data.viewerId
    m.stallSecs = getOr(res.data, "stallSecs", 12)
    m.playlist = withToken(m.global.server + res.data.playlist)
    play()
end sub

sub play()
    c = CreateObject("roSGNode", "ContentNode")
    c.url = m.playlist
    c.streamFormat = "hls"
    c.live = true
    c.title = m.top.findNode("channel").text
    c.HttpHeaders = authHeaders()
    m.video.content = c
    m.video.control = "play"
end sub

sub onState()
    s = m.video.state
    if s = "buffering"
        showBusy("")
        m.bufferedFor = 0
        m.bufferTimer.control = "start"
    else if s = "playing"
        hideBusy()
        m.bufferTimer.control = "stop"
        m.pingTimer.control = "stop"
    else if s = "paused"
        ' A paused player stops fetching the playlist, so keep the viewer counted.
        m.pingTimer.control = "start"
        m.overlay.visible = true
    else if s = "error"
        m.bufferTimer.control = "stop"
        hideBusy()
        m.status.text = "This channel stopped: " + m.video.errorMsg + Chr(10) + "Press OK to try again."
        m.failed = true
    end if
end sub

' Long buffering is reported; past the admin's stall time the server may move
' the channel to its next stream, and the player starts over on it.
sub onBuffering()
    m.bufferedFor = m.bufferedFor + 3
    if m.viewerId = "" then return
    api("POST", "/api/live/viewers/" + m.viewerId + "/buffering", { seconds: m.bufferedFor }, "onBufferReport")
end sub

sub onBufferReport(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok and type(res.data) = "roAssociativeArray" and isTrue(res.data.switched)
        m.video.control = "stop"
        play()
    end if
end sub

sub onPing()
    if m.viewerId <> "" then apiSend("POST", "/api/live/viewers/" + m.viewerId + "/ping", {})
end sub

sub onClosing()
    if not m.top.closing then return
    m.bufferTimer.control = "stop"
    m.pingTimer.control = "stop"
    m.tuneTimer.control = "stop"
    m.video.control = "stop"
    leave(true)
end sub

sub changeChannel(delta as integer)
    n = m.channels.Count()
    if n = 0 then return
    m.index = (m.index + delta + n) mod n
    showChannel()
    m.tuneTimer.control = "stop"
    m.tuneTimer.control = "start"
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "up" or key = "channelup"
        changeChannel(1)
        return true
    else if key = "down" or key = "channeldown"
        changeChannel(-1)
        return true
    else if key = "OK" or key = "play"
        if m.failed
            m.failed = false
            m.status.text = ""
            tune()
        else if m.video.state = "paused"
            m.video.control = "resume"
        else if m.video.state = "playing"
            m.video.control = "pause"
        end if
        showChannel()
        return true
    else if key = "left" or key = "right" or key = "info"
        showChannel()
        return true
    end if
    return false
end function

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
