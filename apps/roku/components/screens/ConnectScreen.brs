sub init()
    m.list = m.top.findNode("list")
    m.status = m.top.findNode("status")
    m.list.observeField("itemSelected", "onSelected")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.found = []
    scanNetwork()
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.list.setFocus(true)
end sub

sub scanNetwork()
    m.found = []
    m.scanning = true
    m.status.text = "Looking for servers on your network..."
    render()
    m.scan = CreateObject("roSGNode", "ScanTask")
    m.scan.observeField("found", "onFound")
    m.scan.observeField("done", "onScanDone")
    m.scan.control = "run"
end sub

sub onFound(event as object)
    s = event.getData()
    for each f in m.found
        if f.url = s.url then return
    end for
    m.found.Push(s)
    render()
end sub

sub onScanDone()
    m.scanning = false
    if m.found.Count() = 0
        m.status.text = "No servers found nearby. If your server is on a different part of the network, or reached through the internet, type its address."
    else
        m.status.text = "Pick your server."
    end if
    render()
end sub

' Found servers first, then the two actions.
sub render()
    root = CreateObject("roSGNode", "ContentNode")
    for each s in m.found
        n = root.createChild("ContentNode")
        n.title = s.name + "   " + s.url.Replace("http://", "").Replace("https://", "")
    end for
    root.createChild("ContentNode").title = "Type an address"
    if not m.scanning then root.createChild("ContentNode").title = "Search again"
    m.list.content = root
end sub

sub onSelected()
    i = m.list.itemSelected
    if i < m.found.Count()
        m.top.navigate = { action: "server", server: m.found[i] }
    else if i = m.found.Count()
        askAddress()
    else
        scanNetwork()
    end if
end sub

sub askAddress()
    d = CreateObject("roSGNode", "StandardKeyboardDialog")
    d.title = "Server address"
    d.message = ["Shown on the Link a device page in BingeBandit, or ask whoever runs it. For example 192.168.1.10 or tv.example.com"]
    d.buttons = ["Connect", "Cancel"]
    if m.typed <> invalid then d.text = m.typed
    d.observeField("buttonSelected", "onDialogButton")
    m.dialog = d
    m.top.getScene().dialog = d
end sub

sub onDialogButton()
    d = m.dialog
    text = d.text
    choice = d.buttonSelected
    d.close = true
    if choice <> 0 or text.Trim() = "" then return
    m.typed = text
    m.status.text = "Checking " + text.Trim() + "..."
    api("GET", "", invalid, "onProbe", { build: "probe", input: text })
end sub

sub onProbe(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok
        m.top.navigate = { action: "server", server: res.data }
    else
        m.status.text = res.error
    end if
end sub
