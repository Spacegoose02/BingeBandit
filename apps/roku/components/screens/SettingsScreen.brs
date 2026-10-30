' The settings passcode keeps account and admin changes behind a 4 to 8 digit
' code. In a household it's each person's PIN, which also guards switching to
' them, and the account owner manages its profiles' PINs here too. A change the
' server refuses with 423 (locked) asks for the passcode and is sent again.

sub init()
    m.page = m.top.findNode("page")
    m.options = m.top.findNode("options")
    m.state = m.top.findNode("state")
    m.hint = m.top.findNode("hint")
    m.msg = m.top.findNode("msg")
    m.pinPad = m.top.findNode("pinPad")
    m.actions = []
    m.members = invalid
    m.passcode = invalid
    m.asking = invalid ' what the PIN pad is for: { kind: "new"|"confirm"|"unlock", ... }
    m.pending = invalid ' the change to send again once unlocked
    m.options.observeField("itemSelected", "onOption")
    m.pinPad.observeField("pin", "onPin")
    m.pinPad.observeField("cancel", "closePad")
    m.top.observeField("focusedChild", "onFocusChanged")
    load()
end sub

sub onFocusChanged()
    if not m.top.hasFocus() then return
    if m.asking <> invalid then m.pinPad.setFocus(true) else m.options.setFocus(true)
end sub

sub load()
    m.members = invalid
    m.passcode = invalid
    api("GET", "/api/household", invalid, "onHousehold")
    api("GET", "/api/me/passcode", invalid, "onPasscode")
end sub

sub onHousehold(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok and type(res.data) = "roArray" then m.members = res.data else m.members = []
    if m.passcode <> invalid then render()
end sub

sub onPasscode(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        m.state.text = ""
        m.hint.text = ""
        showMessage(res.error, true)
        return
    end if
    m.passcode = res.data
    if m.members <> invalid then render()
end sub

sub render()
    you = invalid
    for each mem in m.members
        if isTrue(mem.current) then you = mem
    end for
    household = you <> invalid and m.members.Count() > 1
    owner = household and isTrue(you.isOwner)
    hasCode = isTrue(m.passcode.set)
    unlocked = isTrue(m.passcode.unlocked)
    actions = []
    if household
        m.state.text = "Your PIN (" + you.name + "): " + onOff(hasCode)
        if hasCode
            if owner then guards = "settings, profiles and the server" else guards = "your settings"
            m.hint.text = "Switching to " + you.name + " asks for it, and so does changing " + guards + "."
            actions.Push({ label: "Change your PIN", kind: "new", title: "Choose a 4 to 8 digit PIN", method: "PUT", path: "/api/me/passcode", field: "passcode", done: "Your PIN is changed." })
            actions.Push({ label: "Remove your PIN", method: "DELETE", path: "/api/me/passcode", done: you.name + " has no PIN now." })
        else
            m.hint.text = "Anyone can switch to " + you.name + " now. Add a PIN so only you can."
            actions.Push({ label: "Add a PIN", kind: "new", title: "Choose a 4 to 8 digit PIN", method: "PUT", path: "/api/me/passcode", field: "passcode", done: you.name + " has a PIN now." })
        end if
        if not owner then m.hint.text = m.hint.text + " The account that owns this profile can also change or remove your PIN."
    else
        m.state.text = "Settings passcode: " + onOff(hasCode)
        if hasCode
            m.hint.text = "Changing settings, profiles and the server asks for it first."
            actions.Push({ label: "Change the passcode", kind: "new", title: "Choose a 4 to 8 digit passcode", method: "PUT", path: "/api/me/passcode", field: "passcode", done: "The passcode is changed." })
            actions.Push({ label: "Turn off the passcode", method: "DELETE", path: "/api/me/passcode", done: "The passcode is off." })
        else
            m.hint.text = "Ask for a code before anyone changes settings on this account, so kids can't."
            actions.Push({ label: "Turn on the passcode", kind: "new", title: "Choose a 4 to 8 digit passcode", method: "PUT", path: "/api/me/passcode", field: "passcode", done: "The passcode is on." })
        end if
    end if
    if hasCode and unlocked then actions.Push({ label: "Lock now", method: "POST", path: "/api/me/passcode/lock", done: "Locked." })
    if owner
        for each mem in m.members
            if not isTrue(mem.isOwner)
                path = "/api/household/" + mem.id.ToStr() + "/pin"
                title = "A 4 to 8 digit PIN for " + mem.name
                if isTrue(mem.hasPin)
                    actions.Push({ label: "Change " + mem.name + "'s PIN", kind: "new", title: title, method: "PUT", path: path, field: "pin", done: mem.name + "'s PIN is changed." })
                    actions.Push({ label: "Remove " + mem.name + "'s PIN", method: "PUT", path: path, body: { pin: "" }, done: mem.name + " has no PIN now." })
                else
                    actions.Push({ label: "Add a PIN for " + mem.name, kind: "new", title: title, method: "PUT", path: path, field: "pin", done: mem.name + " has a PIN now." })
                end if
            end if
        end for
    end if
    focus = m.options.itemFocused
    m.actions = actions
    root = CreateObject("roSGNode", "ContentNode")
    for each a in actions
        root.createChild("ContentNode").title = a.label
    end for
    m.options.content = root
    if focus < 0 then focus = 0
    if focus >= actions.Count() then focus = actions.Count() - 1
    m.options.jumpToItem = focus
end sub

function onOff(on as boolean) as string
    if on then return "On"
    return "Off"
end function

sub onOption()
    a = m.actions[m.options.itemSelected]
    if a = invalid then return
    showMessage("", false)
    if getOr(a, "kind", "") = "new"
        openPad({ kind: "new", action: a }, a.title)
    else
        send({ method: a.method, path: a.path, body: getOr(a, "body", invalid), done: a.done })
    end if
end sub

' --- Sending a change --------------------------------------------------------

sub send(change as object)
    m.pending = change
    body = change.body
    if body = invalid and change.method <> "DELETE" then body = {}
    api(change.method, change.path, body, "onSent")
end sub

sub onSent(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.status = 423
        openPad({ kind: "unlock" }, "Enter your settings passcode")
        return
    end if
    change = m.pending
    m.pending = invalid
    if res.ok then showMessage(change.done, false) else showMessage(res.error, true)
    load()
end sub

sub onUnlocked(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        m.pinPad.error = res.error
        return
    end if
    change = m.pending
    closePad()
    if change <> invalid then send(change)
end sub

sub showMessage(text as string, isError as boolean)
    if isError then m.msg.color = "0xF87171FF" else m.msg.color = "0x2DD4BFFF"
    m.msg.text = text
end sub

' --- PIN pad -----------------------------------------------------------------

sub openPad(asking as object, title as string)
    m.asking = asking
    m.pinPad.title = title
    m.pinPad.reset = true
    m.page.visible = false
    m.pinPad.visible = true
    m.pinPad.setFocus(true)
end sub

sub closePad()
    if m.asking <> invalid and m.asking.kind = "unlock" then m.pending = invalid
    m.asking = invalid
    m.pinPad.visible = false
    m.page.visible = true
    m.options.setFocus(true)
end sub

' A new PIN is typed twice, since nothing shows what was pressed.
sub onPin()
    pin = m.pinPad.pin
    a = m.asking
    if a = invalid then return
    if a.kind = "unlock"
        api("POST", "/api/me/passcode/unlock", { passcode: pin }, "onUnlocked")
    else if a.kind = "new"
        openPad({ kind: "confirm", action: a.action, first: pin }, "Enter it again")
    else if a.kind = "confirm"
        if pin <> a.first
            openPad({ kind: "new", action: a.action }, a.action.title)
            m.pinPad.error = "Those didn't match. Try again."
            return
        end if
        act = a.action
        body = {}
        body[act.field] = pin
        closePad()
        send({ method: act.method, path: act.path, body: body, done: act.done })
    end if
end sub
