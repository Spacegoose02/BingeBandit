sub init()
    m.bg = m.top.findNode("bg")
end sub

sub onContent()
    c = m.top.itemContent
    if c = invalid then return
    m.top.findNode("logo").uri = c.logo
    m.top.findNode("number").text = c.number
    m.top.findNode("name").text = c.title
    nowText = c.nowTitle
    if nowText = "" then nowText = "No guide information"
    m.top.findNode("now").text = nowText
    m.top.findNode("next").text = c.nextTitle
    layout()
end sub

sub layout()
    w = m.top.width
    if w = 0 then return
    m.bg.width = w
    rest = w - 670
    m.top.findNode("now").width = rest
    m.top.findNode("next").width = rest
    m.top.findNode("track").width = rest
    c = m.top.itemContent
    p = 0.0
    if c <> invalid then p = c.progress
    if p < 0 then p = 0
    if p > 1 then p = 1
    m.top.findNode("bar").width = rest * p
    m.top.findNode("track").visible = c <> invalid and c.nowTitle <> ""
    m.top.findNode("bar").visible = m.top.findNode("track").visible
end sub

sub onFocus()
    if m.top.focusPercent > 0.5 then m.bg.color = "0x1C2128FF" else m.bg.color = "0x14181DFF"
end sub
