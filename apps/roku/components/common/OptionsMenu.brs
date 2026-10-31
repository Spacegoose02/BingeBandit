sub init()
    m.list = m.top.findNode("list")
    m.list.observeField("itemSelected", "onSelected")
    m.top.observeField("focusedChild", "onFocusChanged")
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.list.setFocus(true)
end sub

sub onTitle()
    m.top.findNode("title").text = m.top.title
end sub

sub onMessage()
    m.top.findNode("message").text = m.top.message
end sub

sub onOptions()
    root = CreateObject("roSGNode", "ContentNode")
    for each o in m.top.options
        root.createChild("ContentNode").title = o
    end for
    m.list.content = root
    f = m.top.focus
    if f < 0 or f >= m.top.options.Count() then f = 0
    m.list.jumpToItem = f
end sub

sub onSelected()
    m.top.chosen = m.list.itemSelected
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return true
    if key = "back" or key = "options"
        m.top.closed = true
        return true
    end if
    ' Nothing behind the panel reacts while it's open.
    return true
end function
