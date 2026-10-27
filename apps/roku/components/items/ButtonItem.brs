sub onContent()
    if m.top.itemContent <> invalid then m.top.findNode("label").text = m.top.itemContent.title
end sub

sub layout()
    for each id in ["bg", "label"]
        n = m.top.findNode(id)
        n.width = m.top.width
        n.height = m.top.height
    end for
    m.top.findNode("label").width = m.top.width - 24
    m.top.findNode("label").translation = [12, 0]
end sub

sub onFocus()
    on = m.top.gridHasFocus and m.top.focusPercent > 0.5
    if on
        m.top.findNode("bg").uri = "pkg:/images/button_focus.9.png"
        m.top.findNode("label").color = "0x04211DFF"
    else
        m.top.findNode("bg").uri = "pkg:/images/button.9.png"
        m.top.findNode("label").color = "0xF2F4F7FF"
    end if
end sub
