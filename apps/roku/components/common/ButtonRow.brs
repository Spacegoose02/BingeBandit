sub init()
    m.grid = m.top.findNode("grid")
    m.grid.observeField("itemSelected", "onSelected")
    m.top.observeField("focusedChild", "onFocusChanged")
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.grid.setFocus(true)
end sub

sub onButtons()
    root = CreateObject("roSGNode", "ContentNode")
    for each b in m.top.buttons
        root.createChild("ContentNode").title = b
    end for
    n = m.top.buttons.Count()
    if n < 1 then n = 1
    m.grid.itemSize = [m.top.buttonWidth, 76]
    m.grid.numColumns = n
    m.grid.content = root
end sub

sub onJump()
    m.grid.jumpToItem = m.top.jumpTo
end sub

sub onSelected()
    m.top.buttonSelected = m.grid.itemSelected
end sub
