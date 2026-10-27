sub init()
    m.grid = m.top.findNode("grid")
    m.picker = m.top.findNode("picker")
    m.pinPad = m.top.findNode("pinPad")
    m.keys = m.top.findNode("keys")
    m.dots = m.top.findNode("dots")
    m.pinError = m.top.findNode("pinError")
    m.error = m.top.findNode("error")
    m.members = []
    m.asking = invalid
    m.digits = ""
    m.grid.observeField("itemSelected", "onMember")
    m.keys.observeField("itemSelected", "onKey")
    keys = CreateObject("roSGNode", "ContentNode")
    for each k in ["1", "2", "3", "4", "5", "6", "7", "8", "9", "Del", "0", "OK"]
        keys.createChild("ContentNode").title = k
    end for
    m.keys.content = keys
    m.top.observeField("params", "onParams")
    m.top.observeField("focusedChild", "onFocusChanged")
end sub

sub onFocusChanged()
    if not m.top.hasFocus() then return
    if m.asking <> invalid then m.keys.setFocus(true) else m.grid.setFocus(true)
end sub

sub onParams()
    members = getOr(m.top.params, "members", invalid)
    if members <> invalid
        showMembers(members)
    else
        api("GET", "/api/household", invalid, "onHousehold")
    end if
end sub

sub onHousehold(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok then showMembers(res.data) else m.error.text = res.error
end sub

sub showMembers(members as object)
    m.members = members
    root = CreateObject("roSGNode", "ContentNode")
    focus = 0
    for i = 0 to members.Count() - 1
        mem = members[i]
        n = root.createChild("ContentNode")
        n.title = mem.name
        avatar = ""
        if getOr(mem, "avatar", "") <> "" then avatar = imageUrl(mem.avatar, "w342")
        n.addFields({ avatar: avatar, hasPin: isTrue(mem.hasPin), isCurrent: isTrue(mem.current) })
        if isTrue(mem.current) then focus = i
    end for
    cols = members.Count()
    if cols > 5 then cols = 5
    if cols < 1 then cols = 1
    m.grid.numColumns = cols
    width = cols * 260 + (cols - 1) * 40
    m.grid.translation = [(1920 - width) / 2, 380]
    m.grid.content = root
    m.grid.jumpToItem = focus
    if m.top.hasFocus() or m.top.isInFocusChain() then m.grid.setFocus(true)
end sub

sub onMember()
    choose(m.members[m.grid.itemSelected], "")
end sub

sub choose(mem as object, pin as string)
    if isTrue(mem.current)
        m.top.navigate = { action: "profileChosen" }
        return
    end if
    if isTrue(mem.hasPin) and pin = ""
        askPin(mem)
        return
    end if
    m.error.text = ""
    api("POST", "/api/household/" + mem.id.ToStr() + "/switch", { pin: pin }, "onSwitched")
end sub

sub onSwitched(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok
        s = currentSession()
        s.token = res.data.token
        s.user = res.data.user
        s.profileChosen = false
        saveSession(s)
        m.top.navigate = { action: "profileChosen" }
        return
    end if
    if m.asking <> invalid and type(res.data) = "roAssociativeArray" and isTrue(res.data.needsPin)
        m.digits = ""
        m.dots.text = ""
        m.pinError.text = "Wrong PIN, try again"
        return
    end if
    closePin()
    m.error.text = res.error
end sub

' --- PIN pad (PINs are 4 to 8 digits, so the built-in 4-digit dialog won't do) ---

sub askPin(mem as object)
    m.asking = mem
    m.digits = ""
    m.dots.text = ""
    m.pinError.text = ""
    m.top.findNode("pinTitle").text = "Enter " + mem.name + "'s PIN"
    m.picker.visible = false
    m.pinPad.visible = true
    m.keys.jumpToItem = 0
    m.keys.setFocus(true)
end sub

sub closePin()
    m.asking = invalid
    m.pinPad.visible = false
    m.picker.visible = true
    m.grid.setFocus(true)
end sub

sub onKey()
    k = m.keys.content.getChild(m.keys.itemSelected).title
    if k = "Del"
        if Len(m.digits) > 0 then m.digits = Left(m.digits, Len(m.digits) - 1)
    else if k = "OK"
        if Len(m.digits) >= 4 then choose(m.asking, m.digits)
        return
    else if Len(m.digits) < 8
        m.digits = m.digits + k
    end if
    m.pinError.text = ""
    m.dots.text = String(Len(m.digits), "*")
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "back"
        if m.asking <> invalid
            closePin()
            return true
        end if
    end if
    return false
end function
