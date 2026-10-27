sub init()
    m.initial = m.top.findNode("initial")
    m.avatar = m.top.findNode("avatar")
    m.name = m.top.findNode("name")
    m.hint = m.top.findNode("hint")
end sub

sub onContent()
    c = m.top.itemContent
    if c = invalid then return
    m.name.text = c.title
    m.initial.text = UCase(Left(c.title, 1))
    m.avatar.uri = c.avatar
    if c.hasPin and not c.isCurrent then m.hint.text = "PIN" else m.hint.text = ""
end sub

sub onFocus()
    if m.top.focusPercent > 0.5 then m.name.color = "0x2DD4BFFF" else m.name.color = "0xF2F4F7FF"
end sub
