sub init()
    m.list = m.top.findNode("list")
    m.status = m.top.findNode("status")
    m.list.observeField("itemSelected", "onSelected")
    m.top.observeField("refresh", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.tick = m.top.findNode("tick")
    m.tick.observeField("fire", "load")
    m.tick.control = "start"
    m.channels = []
    m.status.text = "Loading channels..."
    load()
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.list.setFocus(true)
end sub

sub load()
    if not m.top.visible and m.list.content <> invalid then return
    api("GET", "/api/live/channels", invalid, "onLoaded", { build: "channels" })
end sub

sub onLoaded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        if m.list.content = invalid then m.status.text = res.error
        return
    end if
    m.channels = res.data
    if t.content.getChildCount() = 0 then m.status.text = "No channels yet. An admin adds them in BingeBandit's Live TV settings." else m.status.text = ""
    focus = m.list.itemFocused
    m.list.content = t.content
    if focus > 0 and focus < t.content.getChildCount() then m.list.jumpToItem = focus
end sub

sub onSelected()
    m.top.navigate = { screen: "LivePlayerScreen", params: { channels: m.channels, index: m.list.itemSelected } }
end sub
