sub onContent()
    c = m.top.itemContent
    if c = invalid then return
    m.top.findNode("label").text = c.title
    if c.title = "OK"
        m.top.findNode("bg").color = "0x2DD4BFFF"
        m.top.findNode("label").color = "0x04211DFF"
    end if
end sub
