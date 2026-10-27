sub init()
    m.view = m.top.findNode("view")
    m.view.observeField("navigate", "onNavigate")
    m.view.observeField("backdrop", "onBackdrop")
    m.top.observeField("params", "onParams")
    m.top.observeField("resumed", "onResumed")
    m.top.observeField("focusedChild", "onFocusChanged")
end sub

sub onParams()
    p = m.top.params
    p.columns = 8
    m.view.params = p
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.view.setFocus(true)
end sub

sub onNavigate(event as object)
    m.top.navigate = event.getData()
end sub

sub onBackdrop(event as object)
    m.top.findNode("backdrop").uri = event.getData()
end sub

sub onResumed()
    m.view.refresh = true
end sub
