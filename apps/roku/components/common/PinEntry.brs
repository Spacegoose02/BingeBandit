sub init()
    m.keys = m.top.findNode("keys")
    m.dots = m.top.findNode("dots")
    m.digits = ""
    m.keys.observeField("itemSelected", "onKey")
    keys = CreateObject("roSGNode", "ContentNode")
    for each k in ["1", "2", "3", "4", "5", "6", "7", "8", "9", "Del", "0", "OK"]
        keys.createChild("ContentNode").title = k
    end for
    m.keys.content = keys
    m.top.observeField("focusedChild", "onFocusChanged")
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.keys.setFocus(true)
end sub

sub onTitle()
    m.top.findNode("title").text = m.top.title
end sub

sub onError()
    m.top.findNode("error").text = m.top.error
    ' A wrong PIN starts over.
    if m.top.error <> ""
        m.digits = ""
        m.dots.text = ""
        m.keys.jumpToItem = 0
    end if
end sub

sub onReset()
    m.digits = ""
    m.dots.text = ""
    m.top.error = ""
    m.keys.jumpToItem = 0
end sub

sub onKey()
    k = m.keys.content.getChild(m.keys.itemSelected).title
    if k = "Del"
        if Len(m.digits) > 0 then m.digits = Left(m.digits, Len(m.digits) - 1)
    else if k = "OK"
        if Len(m.digits) >= 4 then m.top.pin = m.digits
        return
    else if Len(m.digits) < 8
        m.digits = m.digits + k
    end if
    m.top.findNode("error").text = ""
    m.dots.text = String(Len(m.digits), "*")
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if press and key = "back"
        m.top.cancel = true
        return true
    end if
    return false
end function
