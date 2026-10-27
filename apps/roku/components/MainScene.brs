' Opens the right first screen and keeps a stack of screens. Every screen
' asks for the next step through its `navigate` field; Back pops the stack.

sub init()
    m.top.backgroundColor = "0x0B0D10"
    m.top.backgroundURI = ""
    m.screens = m.top.findNode("screens")
    m.stack = []
    ' A screen that comes back refreshes a moment later, once the closing
    ' screen's last request (the player's final position) has landed.
    m.resumeTimer = m.top.createChild("Timer")
    m.resumeTimer.duration = 1
    m.resumeTimer.observeField("fire", "onResumeTimer")
    m.global.observeField("unauthorized", "onUnauthorized")
    if loadSession()
        afterSignIn()
    else
        startSignIn()
    end if
end sub

' Signed out: go straight to the pairing code for the last server, or find one.
sub startSignIn()
    server = lastServer()
    if server <> invalid
        replaceAll("SignInScreen", { server: server })
    else
        replaceAll("ConnectScreen", {})
    end if
end sub

' Like the Android TV app: skip "Who's watching?" when the TV was linked to a
' profile, there's only one person, or the account opens as someone without a PIN.
sub afterSignIn()
    if m.global.profileChosen
        replaceAll("HomeScreen", {})
        return
    end if
    replaceAll("LoadingScreen", {})
    api("GET", "/api/household", invalid, "onHousehold")
end sub

sub onHousehold(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    members = []
    if res.ok and type(res.data) = "roArray" then members = res.data
    if members.Count() < 2
        replaceAll("HomeScreen", {})
        return
    end if
    auto = invalid
    for each mem in members
        if isTrue(mem.auto) then auto = mem
    end for
    if auto <> invalid and isTrue(auto.current)
        replaceAll("HomeScreen", {})
    else if auto <> invalid and not isTrue(auto.hasPin)
        api("POST", "/api/household/" + auto.id.ToStr() + "/switch", { pin: "" }, "onAutoSwitch")
    else
        replaceAll("ProfilesScreen", { members: members, cancelable: false })
    end if
end sub

sub onAutoSwitch(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok
        s = currentSession()
        s.token = res.data.token
        s.user = res.data.user
        saveSession(s)
        replaceAll("HomeScreen", {})
    else
        replaceAll("ProfilesScreen", { cancelable: false })
    end if
end sub

sub onUnauthorized()
    if not m.global.unauthorized then return
    m.global.unauthorized = false
    saveSession(invalid)
    startSignIn()
end sub

' --- The stack ---------------------------------------------------------------

function makeScreen(name as string, params as object) as object
    s = CreateObject("roSGNode", name)
    s.params = params
    s.observeField("navigate", "onNavigate")
    return s
end function

sub push(name as string, params as object)
    if m.stack.Count() > 0 then m.stack.Peek().visible = false
    s = makeScreen(name, params)
    m.screens.appendChild(s)
    m.stack.Push(s)
    s.setFocus(true)
end sub

sub pop()
    if m.stack.Count() < 2 then return
    s = m.stack.Pop()
    s.unobserveField("navigate")
    s.closing = true
    m.screens.removeChild(s)
    top = m.stack.Peek()
    top.visible = true
    top.setFocus(true)
    m.resumeTimer.control = "stop"
    m.resumeTimer.control = "start"
end sub

sub onResumeTimer()
    if m.stack.Count() > 0 then m.stack.Peek().resumed = true
end sub

sub replaceAll(name as string, params as object)
    for each s in m.stack
        s.unobserveField("navigate")
        s.closing = true
    end for
    m.screens.removeChildrenIndex(m.screens.getChildCount(), 0)
    m.stack = []
    push(name, params)
end sub

sub onNavigate(event as object)
    nav = event.getData()
    if nav = invalid then return
    action = getOr(nav, "action", "push")
    if action = "push"
        push(nav.screen, getOr(nav, "params", {}))
    else if action = "replace"
        ' Swap the top screen (the player moving on to the next episode).
        s = m.stack.Pop()
        s.unobserveField("navigate")
        s.closing = true
        m.screens.removeChild(s)
        push(nav.screen, getOr(nav, "params", {}))
    else if action = "back"
        pop()
    else if action = "server"
        rememberServer(nav.server)
        push("SignInScreen", { server: nav.server })
    else if action = "connect"
        replaceAll("ConnectScreen", {})
    else if action = "signedIn"
        saveSession(nav.session)
        afterSignIn()
    else if action = "profiles"
        push("ProfilesScreen", { cancelable: true })
    else if action = "profileChosen"
        replaceAll("HomeScreen", {})
    else if action = "signOut"
        api("POST", "/api/auth/logout", {}, "onFireAndForget", { token: m.global.token })
        saveSession(invalid)
        startSignIn()
    end if
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "back" and m.stack.Count() > 1
        pop()
        return true
    end if
    return false
end function
