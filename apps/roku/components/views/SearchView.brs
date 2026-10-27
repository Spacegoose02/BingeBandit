sub init()
    m.keyboard = m.top.findNode("keyboard")
    m.grid = m.top.findNode("grid")
    m.status = m.top.findNode("status")
    m.timer = m.top.findNode("debounce")
    m.status.text = "Type a title, a person or a genre."
    m.keyboard.observeField("text", "onText")
    m.timer.observeField("fire", "search")
    m.grid.observeField("itemSelected", "onSelected")
    m.grid.observeField("itemFocused", "onFocused")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.searched = ""
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.keyboard.setFocus(true)
end sub

sub onText()
    m.timer.control = "stop"
    m.timer.control = "start"
end sub

sub search()
    q = m.keyboard.text.Trim()
    if q = m.searched then return
    m.searched = q
    if q = ""
        m.grid.content = invalid
        m.status.text = "Type a title, a person or a genre."
        return
    end if
    xfer = CreateObject("roUrlTransfer")
    api("GET", "/api/search?q=" + xfer.Escape(q), invalid, "onResults", { build: "items", query: q })
end sub

sub onResults(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    if t.request.query <> m.searched then return ' an older search answered late
    res = event.getData()
    if not res.ok
        m.status.text = res.error
        return
    end if
    m.grid.content = t.content
    if t.content.getChildCount() = 0 then m.status.text = "Nothing matches " + Chr(34) + m.searched + Chr(34) + "." else m.status.text = ""
end sub

sub onFocused()
    c = m.grid.content.getChild(m.grid.itemFocused)
    if c <> invalid then m.top.backdrop = c.backdrop
end sub

sub onSelected()
    m.top.navigate = openItem(m.grid.content.getChild(m.grid.itemSelected))
end sub

' Right from the keyboard's edge moves to the results, Left comes back.
function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "right" and m.keyboard.isInFocusChain() and m.grid.content <> invalid and m.grid.content.getChildCount() > 0
        m.grid.setFocus(true)
        return true
    end if
    if key = "left" and m.grid.isInFocusChain()
        m.keyboard.setFocus(true)
        return true
    end if
    return false
end function
