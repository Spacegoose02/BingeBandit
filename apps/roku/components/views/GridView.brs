sub init()
    m.grid = m.top.findNode("grid")
    m.status = m.top.findNode("status")
    m.grid.observeField("itemSelected", "onSelected")
    m.grid.observeField("itemFocused", "onFocused")
    m.top.observeField("params", "onParams")
    m.top.observeField("refresh", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.grid.setFocus(true)
end sub

' params: { title, path, build: items|library, columns? }
sub onParams()
    p = m.top.params
    m.top.findNode("title").text = getOr(p, "title", "")
    cols = getOr(p, "columns", 7)
    m.grid.numColumns = cols
    m.status.text = "Loading..."
    load()
end sub

sub load()
    p = m.top.params
    if p = invalid or p.path = invalid then return
    api("GET", p.path, invalid, "onLoaded", { build: getOr(p, "build", "items") })
end sub

sub onLoaded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        if m.grid.content = invalid then m.status.text = res.error
        return
    end if
    content = t.content
    n = content.getChildCount()
    if n = 0
        m.status.text = getOr(m.top.params, "empty", "Nothing here yet.")
        m.top.findNode("count").text = ""
    else
        m.status.text = ""
        if n = 1 then m.top.findNode("count").text = "1 title" else m.top.findNode("count").text = n.ToStr() + " titles"
    end if
    focus = m.grid.itemFocused
    m.grid.content = content
    if focus > 0 and focus < n then m.grid.jumpToItem = focus
end sub

sub onFocused()
    if m.grid.content = invalid then return
    c = m.grid.content.getChild(m.grid.itemFocused)
    if c <> invalid then m.top.backdrop = c.backdrop
end sub

sub onSelected()
    m.top.navigate = openItem(m.grid.content.getChild(m.grid.itemSelected))
end sub
