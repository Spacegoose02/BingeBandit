sub init()
    m.menu = m.top.findNode("menu")
    m.views = m.top.findNode("views")
    m.backdrop = m.top.findNode("backdrop")
    m.switchTimer = m.top.findNode("switchTimer")
    m.entries = []
    m.cache = {}
    m.current = invalid
    m.currentId = ""
    m.inContent = false
    m.menu.observeField("itemFocused", "onMenuFocused")
    m.menu.observeField("itemSelected", "onMenuSelected")
    m.switchTimer.observeField("fire", "showFocusedEntry")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.top.observeField("resumed", "onResumed")
    user = m.global.user
    buildMenu(user, [])
    api("GET", "/api/me", invalid, "onMe")
end sub

sub onFocusChanged()
    if not m.top.hasFocus() then return
    if m.current <> invalid and isTrue(m.inContent) then m.current.setFocus(true) else m.menu.setFocus(true)
end sub

' The account's flags can change on the server (kid mode, Live TV), so ask
' fresh, then the libraries this person can see.
sub onMe(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if res.ok
        s = currentSession()
        s.user = res.data
        saveSession(s)
        ' In the background: the server's other addresses, for when this one stops answering.
        api("GET", "", invalid, "onFireAndForget", { build: "learn" })
    end if
    api("GET", "/api/libraries", invalid, "onLibraries")
end sub

sub onLibraries(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    libs = []
    if res.ok and type(res.data) = "roArray" then libs = res.data
    buildMenu(m.global.user, libs)
end sub

sub buildMenu(user as object, libs as object)
    name = getOr(user, "profileName", "")
    if name = "" then name = getOr(user, "username", "")
    m.top.findNode("who").text = name
    entries = [{ id: "home", label: "Home" }, { id: "search", label: "Search" }]
    if isTrue(user.kid)
        entries.Push({ id: "kidsMovies", label: "Approved Movies" })
        entries.Push({ id: "kidsShows", label: "Approved Shows" })
    end if
    for each lib in libs
        entries.Push({ id: "lib:" + lib.id.ToStr(), label: lib.name, library: lib })
    end for
    if getOr(user, "liveTV", true) <> false then entries.Push({ id: "live", label: "Live TV" })
    entries.Push({ id: "settings", label: "Settings" })
    entries.Push({ id: "profiles", label: "Switch profile" })
    entries.Push({ id: "signout", label: "Sign out" })
    m.entries = entries
    root = CreateObject("roSGNode", "ContentNode")
    focus = 0
    for i = 0 to entries.Count() - 1
        root.createChild("ContentNode").title = entries[i].label
        if entries[i].id = m.currentId then focus = i
    end for
    m.menu.content = root
    m.menu.jumpToItem = focus
    if m.current = invalid then showEntry(entries[0])
end sub

sub onMenuFocused()
    m.switchTimer.control = "stop"
    m.switchTimer.control = "start"
end sub

sub showFocusedEntry()
    e = m.entries[m.menu.itemFocused]
    if e <> invalid and e.id <> "settings" and e.id <> "profiles" and e.id <> "signout" then showEntry(e)
end sub

sub onMenuSelected()
    e = m.entries[m.menu.itemSelected]
    if e.id = "settings"
        m.top.navigate = { screen: "SettingsScreen" }
    else if e.id = "profiles"
        m.top.navigate = { action: "profiles" }
    else if e.id = "signout"
        confirmSignOut()
    else
        showEntry(e)
        enterContent()
    end if
end sub

' Panels are kept once made, so moving through the menu doesn't reload them.
sub showEntry(e as object)
    if e.id = m.currentId then return
    if m.current <> invalid then m.current.visible = false
    v = m.cache[e.id]
    if v = invalid
        v = makeView(e)
        v.observeField("navigate", "onViewNavigate")
        v.observeField("backdrop", "onBackdrop")
        m.views.appendChild(v)
        m.cache[e.id] = v
    end if
    v.visible = true
    m.current = v
    m.currentId = e.id
    m.backdrop.uri = v.backdrop
end sub

function makeView(e as object) as object
    if e.id = "home" then return CreateObject("roSGNode", "RowsView")
    if e.id = "search" then return CreateObject("roSGNode", "SearchView")
    if e.id = "live" then return CreateObject("roSGNode", "GuideView")
    v = CreateObject("roSGNode", "GridView")
    if e.id = "kidsMovies"
        v.params = { title: "Approved Movies", path: "/api/kids/titles?kind=movie", build: "items", empty: "No movies approved yet. A parent approves them in BingeBandit." }
    else if e.id = "kidsShows"
        v.params = { title: "Approved Shows", path: "/api/kids/titles?kind=show", build: "items", empty: "No shows approved yet. A parent approves them in BingeBandit." }
    else
        v.params = { title: e.library.name, path: "/api/libraries/" + e.library.id.ToStr() + "/items", build: "library" }
    end if
    return v
end function

sub enterContent()
    if m.current = invalid then return
    m.inContent = true
    m.current.setFocus(true)
end sub

sub onViewNavigate(event as object)
    m.top.navigate = event.getData()
end sub

sub onBackdrop(event as object)
    if m.current <> invalid and m.current.isSameNode(event.getRoSGNode()) then m.backdrop.uri = event.getData()
end sub

' Back from the player or a details page: progress and Continue Watching changed.
sub onResumed()
    if m.current <> invalid then m.current.refresh = true
    for each id in m.cache
        v = m.cache[id]
        if not v.isSameNode(m.current) and id = "home" then v.refresh = true
    end for
end sub

sub confirmSignOut()
    d = CreateObject("roSGNode", "StandardMessageDialog")
    d.title = "Sign out?"
    d.message = ["This Roku will need a new code to sign in again."]
    d.buttons = ["Sign out", "Cancel"]
    d.observeField("buttonSelected", "onSignOutButton")
    m.dialog = d
    m.top.getScene().dialog = d
end sub

sub onSignOutButton()
    choice = m.dialog.buttonSelected
    m.dialog.close = true
    if choice = 0 then m.top.navigate = { action: "signOut" }
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "right" and m.menu.hasFocus()
        enterContent()
        return true
    end if
    if (key = "left" or key = "back") and m.current <> invalid and m.current.isInFocusChain()
        m.inContent = false
        m.menu.setFocus(true)
        return true
    end if
    if key = "options" and m.current <> invalid
        m.current.refresh = true
        return true
    end if
    return false
end function
