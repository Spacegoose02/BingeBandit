sub init()
    m.rows = m.top.findNode("rows")
    m.status = m.top.findNode("status")
    m.rows.observeField("rowItemSelected", "onSelected")
    m.rows.observeField("rowItemFocused", "onFocused")
    m.top.observeField("refresh", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.status.text = "Loading..."
    load()
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.rows.setFocus(true)
end sub

sub load()
    api("GET", "/api/home", invalid, "onLoaded", { build: "home" })
end sub

sub onLoaded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        if m.rows.content = invalid then m.status.text = res.error
        return
    end if
    content = t.content
    if content.getChildCount() = 0
        m.status.text = "Nothing to watch yet. Once your server has scanned a library, it shows up here."
    else
        m.status.text = ""
    end if
    sizes = []
    heights = []
    for i = 0 to content.getChildCount() - 1
        if content.getChild(i).wide
            sizes.Push([380, 280])
            heights.Push(350)
        else
            sizes.Push([200, 366])
            heights.Push(436)
        end if
    end for
    ' Keep the place when refreshing after the player.
    focus = m.rows.rowItemFocused
    m.rows.rowItemSize = sizes
    m.rows.rowHeights = heights
    m.rows.content = content
    if focus <> invalid and focus.Count() = 2 and focus[0] < content.getChildCount()
        m.rows.jumpToRowItem = focus
    end if
end sub

function focusedContent() as dynamic
    f = m.rows.rowItemFocused
    if f = invalid or f.Count() < 2 or m.rows.content = invalid then return invalid
    row = m.rows.content.getChild(f[0])
    if row = invalid then return invalid
    return row.getChild(f[1])
end function

sub onFocused()
    c = focusedContent()
    if c <> invalid then m.top.backdrop = c.backdrop
end sub

sub onSelected()
    f = m.rows.rowItemSelected
    c = m.rows.content.getChild(f[0]).getChild(f[1])
    m.top.navigate = openItem(c)
end sub
