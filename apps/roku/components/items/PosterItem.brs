sub init()
    m.art = m.top.findNode("art")
    m.fallback = m.top.findNode("fallback")
    m.track = m.top.findNode("track")
    m.bar = m.top.findNode("bar")
    m.badge = m.top.findNode("badge")
    m.badgeText = m.top.findNode("badgeText")
    m.title = m.top.findNode("title")
    m.subLabel = m.top.findNode("sub")
end sub

sub onContent()
    c = m.top.itemContent
    if c = invalid then return
    m.art.uri = c.poster
    m.fallback.text = c.title
    m.fallback.visible = c.poster = ""
    m.title.text = c.title
    m.subLabel.text = c.subtitle
    m.track.visible = c.progress > 0
    m.bar.visible = c.progress > 0
    n = c.unwatched
    m.badge.visible = n <> invalid and n > 0 and (c.kind = "show" or c.kind = "season")
    if m.badge.visible then m.badgeText.text = n.ToStr()
    layout()
end sub

sub layout()
    w = m.top.width
    h = m.top.height
    if w = 0 or h = 0 then return
    artH = h - 66
    m.art.width = w
    m.top.findNode("tile").width = w
    m.top.findNode("tile").height = artH
    m.art.height = artH
    m.fallback.width = w - 20
    m.fallback.height = artH
    m.fallback.translation = [10, 0]
    m.track.width = w
    m.track.translation = [0, artH - 8]
    c = m.top.itemContent
    p = 0.0
    if c <> invalid then p = c.progress
    if p > 1 then p = 1
    m.bar.width = w * p
    m.bar.translation = [0, artH - 8]
    m.badge.translation = [w - 44, 0]
    m.title.width = w
    m.title.translation = [0, artH + 8]
    m.subLabel.width = w
    m.subLabel.translation = [0, artH + 38]
end sub

sub onFocus()
    if m.top.focusPercent > 0.5 then m.title.color = "0x2DD4BFFF" else m.title.color = "0xF2F4F7FF"
end sub
