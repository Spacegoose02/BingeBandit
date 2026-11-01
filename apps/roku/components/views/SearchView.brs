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
    m.items = invalid
    m.requestable = invalid
    api("GET", "/api/search?q=" + xfer.Escape(q), invalid, "onResults", { build: "items", query: q })
    ' Titles the server doesn't have, to request (nothing when requests are off).
    api("GET", "/api/requests/search?q=" + xfer.Escape(q), invalid, "onRequestable", { build: "requestable", query: q })
end sub

sub onRequestable(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    if t.request.query <> m.searched then return
    if event.getData().ok then m.requestable = t.content
    showResults()
end sub

' The library's results, then the titles that could be requested.
sub showResults()
    if m.items = invalid then return
    content = CreateObject("roSGNode", "ContentNode")
    for each c in m.items.getChildren(-1, 0)
        content.appendChild(c.clone(true))
    end for
    if m.requestable <> invalid
        for each c in m.requestable.getChildren(-1, 0)
            content.appendChild(c.clone(true))
        end for
    end if
    focused = m.grid.itemFocused
    m.grid.content = content
    if focused > 0 and focused < content.getChildCount() then m.grid.jumpToItem = focused
    if content.getChildCount() = 0 then m.status.text = "Nothing matches " + Chr(34) + m.searched + Chr(34) + "." else m.status.text = ""
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
    m.items = t.content
    showResults()
end sub

sub onFocused()
    c = m.grid.content.getChild(m.grid.itemFocused)
    if c <> invalid then m.top.backdrop = c.backdrop
end sub

sub onSelected()
    c = m.grid.content.getChild(m.grid.itemSelected)
    if c = invalid then return
    if c.kind = "request"
        if c.subtitle <> "Not here · OK to request" and c.subtitle <> "Others asked · OK for you too" then return
        c.subtitle = "Asking…"
        body = { kind: c.tmdbKind, tmdbId: c.tmdbId, title: c.title, poster: c.posterPath, overview: c.overview }
        if c.year > 0 then body.year = c.year
        m.asking = c
        api("POST", "/api/requests", body, "onAsked")
        return
    end if
    m.top.navigate = openItem(c)
end sub

sub onAsked(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if m.asking = invalid then return
    if res.ok
        if res.data <> invalid and getOr(res.data, "status", "") = "parent"
            m.asking.subtitle = "Asked a grown-up"
        else
            m.asking.subtitle = "Requested"
        end if
    else
        m.asking.subtitle = res.error
    end if
    m.asking = invalid
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
