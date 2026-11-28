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
    m.menu = m.top.findNode("menu")
    m.menu.observeField("chosen", "onLibraryChosen")
    m.menu.observeField("closed", "closeLibraries")
    m.searched = ""
end sub

sub onFocusChanged()
    if not m.top.hasFocus() then return
    if m.menu.visible then m.menu.setFocus(true) else m.keyboard.setFocus(true)
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
        if m.asking <> invalid then return
        m.askingLabel = c.subtitle
        c.subtitle = "Asking…"
        m.asking = c
        ' First, which libraries it could go in (Movies, Kids Movies...).
        api("GET", "/api/requests/title/" + c.tmdbKind + "/" + c.tmdbId.ToStr(), invalid, "onLibraries")
        return
    end if
    m.top.navigate = openItem(c)
end sub

' More than one library to pick from (and nobody picked yet): ask which;
' otherwise ask for it right away. Older servers send no libraries.
sub onLibraries(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if m.asking = invalid then return
    m.libraries = []
    first = 0
    if res.ok and type(res.data) = "roAssociativeArray"
        if type(res.data.libraries) = "roArray" then m.libraries = res.data.libraries
        first = getOr(res.data, "library", 0)
        req = invalid
        if type(res.data.title) = "roAssociativeArray" then req = res.data.title.request
        if type(req) = "roAssociativeArray" and getOr(req, "status", "") <> "declined" and getOr(req, "libraryId", 0) > 0 then m.libraries = []
    end if
    if m.libraries.Count() < 2
        sendRequest(first)
        return
    end if
    names = []
    focus = 0
    for i = 0 to m.libraries.Count() - 1
        names.Push(getOr(m.libraries[i], "name", ""))
        if getOr(m.libraries[i], "id", 0) = first then focus = i
    end for
    m.menu.title = "Request " + m.asking.title
    m.menu.message = "Which library should it go in?"
    m.menu.focus = focus
    m.menu.options = names
    m.menu.visible = true
    m.menu.setFocus(true)
end sub

sub onLibraryChosen()
    if m.asking = invalid or not m.menu.visible then return
    i = m.menu.chosen
    if i < 0 or i >= m.libraries.Count() then return
    m.menu.visible = false
    m.grid.setFocus(true)
    sendRequest(getOr(m.libraries[i], "id", 0))
end sub

' Back from the library menu: nothing is asked for.
sub closeLibraries()
    m.menu.visible = false
    if m.asking <> invalid then m.asking.subtitle = m.askingLabel
    m.asking = invalid
    m.grid.setFocus(true)
end sub

sub sendRequest(libraryId as integer)
    c = m.asking
    body = { kind: c.tmdbKind, tmdbId: c.tmdbId, title: c.title, poster: c.posterPath, overview: c.overview }
    if c.year > 0 then body.year = c.year
    if libraryId > 0 then body.libraryId = libraryId
    api("POST", "/api/requests", body, "onAsked")
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
