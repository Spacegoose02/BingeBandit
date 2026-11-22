sub init()
    m.rows = m.top.findNode("rows")
    m.status = m.top.findNode("status")
    m.menu = m.top.findNode("menu")
    m.menu.observeField("chosen", "onMenuChosen")
    m.menu.observeField("closed", "closeMenu")
    m.rows.observeField("rowItemSelected", "onSelected")
    m.rows.observeField("rowItemFocused", "onFocused")
    m.top.observeField("refresh", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.status.text = "Loading..."
    load()
end sub

sub onFocusChanged()
    if not m.top.hasFocus() then return
    if m.menu.visible then m.menu.setFocus(true) else m.rows.setFocus(true)
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

' --- Remove from Continue Watching (the * key) -------------------------------

function onKeyEvent(key as string, press as boolean) as boolean
    if not press or key <> "options" or m.menu.visible then return false
    f = m.rows.rowItemFocused
    if f = invalid or f.Count() < 2 or m.rows.content = invalid then return false
    row = m.rows.content.getChild(f[0])
    c = focusedContent()
    ' Anywhere else, * refreshes Home (HomeScreen).
    if row = invalid or c = invalid or not isTrue(row.inContinue) then return false
    m.menuItem = c
    m.menu.title = c.title
    m.menu.message = ""
    m.menu.focus = 0
    m.menu.options = ["Remove from Continue Watching", "Cancel"]
    m.menu.visible = true
    m.menu.setFocus(true)
    return true
end function

sub onMenuChosen()
    if m.menu.chosen <> 0 or m.menuItem = invalid
        closeMenu()
        return
    end if
    m.menu.message = "Removing..."
    api("DELETE", "/api/continue/" + m.menuItem.itemId.ToStr(), invalid, "onRemoved")
end sub

sub onRemoved(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        m.menu.message = res.error
        return
    end if
    closeMenu()
    load()
end sub

sub closeMenu()
    m.menu.visible = false
    m.menuItem = invalid
    m.rows.setFocus(true)
end sub
