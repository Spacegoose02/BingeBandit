sub init()
    m.code = m.top.findNode("code")
    m.url = m.top.findNode("url")
    m.status = m.top.findNode("status")
    m.buttons = m.top.findNode("buttons")
    m.buttons.buttons = ["Get a new code", "Different server"]
    m.buttons.observeField("buttonSelected", "onButton")
    m.timer = m.top.findNode("poll")
    m.timer.observeField("fire", "poll")
    m.top.observeField("params", "onParams")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.top.observeField("closing", "onClosing")
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.buttons.setFocus(true)
end sub

sub onParams()
    m.server = m.top.params.server
    m.top.findNode("title").text = "Sign in to " + m.server.name
    startLink()
end sub

function deviceName() as string
    di = CreateObject("roDeviceInfo")
    name = di.GetFriendlyName()
    if name = invalid or name = "" then name = "Roku " + di.GetModelDisplayName()
    if Len(name) > 50 then name = Left(name, 50)
    return name
end function

sub startLink()
    m.timer.control = "stop"
    m.pollToken = ""
    m.code.text = "------"
    m.url.text = "..."
    m.status.text = ""
    api("POST", "/api/link/start", { device: deviceName() }, "onStarted", { server: m.server.url })
end sub

sub onStarted(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        m.status.text = res.error
        return
    end if
    c = res.data.code
    m.code.text = Left(c, 3) + "-" + Mid(c, 4)
    link = res.data.linkUrl.Replace("http://", "").Replace("https://", "")
    q = Instr(1, link, "?")
    if q > 0 then link = Left(link, q - 1)
    m.url.text = link
    m.status.text = "Waiting for you to approve it. The code works for 10 minutes."
    m.pollToken = res.data.pollToken
    m.timer.control = "start"
end sub

sub poll()
    if m.pollToken = "" or m.top.closing then return
    api("GET", "/api/link/poll?token=" + m.pollToken, invalid, "onPoll", { server: m.server.url })
end sub

sub onPoll(event as object)
    doneTask(event.getRoSGNode())
    if m.top.closing then return
    res = event.getData()
    if res.status = 200 and type(res.data) = "roAssociativeArray" and res.data.token <> invalid
        m.pollToken = ""
        m.top.navigate = { action: "signedIn", session: { server: m.server.url, token: res.data.token, user: res.data.user, profileChosen: isTrue(res.data.profileChosen) } }
    else if res.status = 202 or res.status = 0
        ' Still waiting (or a passing network blip): ask again shortly.
        m.timer.control = "start"
    else
        m.pollToken = ""
        m.status.text = res.error
    end if
end sub

sub onButton()
    if m.buttons.buttonSelected = 0
        startLink()
    else
        m.top.navigate = { action: "connect" }
    end if
end sub

sub onClosing()
    m.timer.control = "stop"
end sub
