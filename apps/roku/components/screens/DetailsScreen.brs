sub init()
    m.buttons = m.top.findNode("buttons")
    m.children = m.top.findNode("children")
    m.status = m.top.findNode("status")
    m.actions = []
    m.detail = invalid
    m.onChildren = false
    m.buttons.observeField("buttonSelected", "onButton")
    m.children.observeField("rowItemSelected", "onChild")
    m.top.observeField("params", "load")
    m.top.observeField("resumed", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.status.text = "Loading..."
end sub

sub onFocusChanged()
    if not m.top.hasFocus() then return
    if m.onChildren and m.children.content <> invalid then m.children.setFocus(true) else m.buttons.setFocus(true)
end sub

sub load()
    api("GET", "/api/items/" + m.top.params.id.ToStr(), invalid, "onLoaded", { build: "details" })
end sub

sub onLoaded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        m.status.text = res.error
        return
    end if
    m.status.text = ""
    d = res.data
    m.detail = d
    it = d.item
    show = getOr(d, "show", invalid)
    kind = it.kind

    art = firstOf(it, ["backdrop", "showBackdrop"])
    if art = invalid and show <> invalid then art = getOr(show, "backdrop", invalid)
    m.top.findNode("backdrop").uri = imageUrl(art, "w1280")
    poster = firstOf(it, ["poster", "showPoster"])
    if poster = invalid and show <> invalid then poster = getOr(show, "poster", invalid)
    m.top.findNode("poster").uri = imageUrl(poster, "w500")

    kicker = ""
    if kind = "episode" or kind = "season" then kicker = getOr(it, "showTitle", "")
    if kicker = "" and show <> invalid then kicker = getOr(show, "title", "")
    m.top.findNode("kicker").text = kicker
    title = it.title
    if kind = "episode" then title = joinStrings([episodeCode(it), it.title], " · ")
    m.top.findNode("title").text = title

    facts = []
    if it.year <> invalid then facts.Push(it.year.ToStr())
    facts.Push(getOr(it, "contentRating", ""))
    facts.Push(runtimeLabel(getOr(it, "runtime", 0)))
    if it.rating <> invalid and it.rating > 0 then facts.Push("★ " + Left(Str(it.rating).Trim(), 3))
    genres = getOr(it, "genres", [])
    top = []
    for each g in genres
        if top.Count() < 3 then top.Push(g)
    end for
    facts.Push(joinStrings(top, ", "))
    if kind = "show"
        n = getOr(d, "children", []).Count()
        if n = 1
            facts.Push("1 season")
        else if n > 1
            facts.Push(n.ToStr() + " seasons")
        end if
    end if
    m.top.findNode("meta").text = joinStrings(facts, "   ·   ")
    overview = getOr(it, "overview", "")
    if overview = "" then overview = getOr(it, "tagline", "")
    m.top.findNode("overview").text = overview

    buildButtons(d)

    row = t.content
    if row.getChildCount() > 0
        rowNode = row.getChild(0)
        if kind = "season"
            m.children.rowItemSize = [[380, 280]]
            m.children.itemSize = [1720, 340]
        else
            m.children.rowItemSize = [[170, 321]]
            m.children.itemSize = [1720, 380]
        end if
        focus = m.children.rowItemFocused
        m.children.content = row
        m.children.visible = true
        if focus <> invalid and focus.Count() = 2 and focus[1] < rowNode.getChildCount() then m.children.jumpToRowItem = [0, focus[1]]
    else
        m.children.content = invalid
        m.children.visible = false
    end if
    if m.top.isInFocusChain() and not m.children.isInFocusChain() then m.buttons.setFocus(true)
end sub

' What to play from this page: the item itself, a show's next episode, or a
' season's first unfinished episode.
function playTarget(d as object) as dynamic
    it = d.item
    if it.kind = "movie" or it.kind = "episode" then return it
    if it.kind = "show" then return getOr(d, "upNext", invalid)
    for each c in getOr(d, "children", [])
        if c.watch = invalid or not isTrue(c.watch.watched) then return c
    end for
    kids = getOr(d, "children", [])
    if kids.Count() > 0 then return kids[0]
    return invalid
end function

function resumeAt(it as object) as dynamic
    w = it.watch
    if w = invalid or isTrue(w.watched) or w.position = invalid then return 0
    if w.position < 30 then return 0
    if w.duration <> invalid and w.duration > 0 and w.position > w.duration - 30 then return 0
    return w.position
end function

sub buildButtons(d as object)
    it = d.item
    labels = []
    m.actions = []
    target = playTarget(d)
    if target <> invalid
        prefix = ""
        if target.id <> it.id then prefix = episodeCode(target) + "  "
        at = resumeAt(target)
        if at > 0
            if prefix <> "" then labels.Push("Resume " + prefix) else labels.Push("Resume from " + clockTime(at))
            m.actions.Push({ act: "play", id: target.id, start: at })
            labels.Push("Start over")
            m.actions.Push({ act: "play", id: target.id, start: 0 })
        else
            labels.Push("Play " + prefix.Trim())
            m.actions.Push({ act: "play", id: target.id, start: 0 })
        end if
    end if
    if it.watch <> invalid and isTrue(it.watch.watched)
        labels.Push("Mark unwatched")
        m.actions.Push({ act: "unwatched" })
    else
        labels.Push("Mark watched")
        m.actions.Push({ act: "watched" })
    end if
    if it.kind = "movie" or it.kind = "show"
        if isTrue(it.onWatchlist)
            labels.Push("Remove from list")
            m.actions.Push({ act: "unlist" })
        else
            labels.Push("Add to watchlist")
            m.actions.Push({ act: "list" })
        end if
    end if
    for i = 0 to labels.Count() - 1
        labels[i] = labels[i].Trim()
    end for
    m.buttons.buttons = labels
end sub

sub onButton()
    a = m.actions[m.buttons.buttonSelected]
    id = m.detail.item.id.ToStr()
    if a.act = "play"
        m.top.navigate = { screen: "PlayerScreen", params: { id: a.id, start: a.start } }
    else if a.act = "watched" or a.act = "unwatched"
        api("POST", "/api/items/" + id + "/" + a.act, {}, "onChanged")
    else if a.act = "list"
        api("POST", "/api/watchlist/" + id, {}, "onChanged")
    else if a.act = "unlist"
        api("DELETE", "/api/watchlist/" + id, invalid, "onChanged")
    end if
end sub

sub onChanged(event as object)
    doneTask(event.getRoSGNode())
    load()
end sub

sub onChild()
    f = m.children.rowItemSelected
    c = m.children.content.getChild(f[0]).getChild(f[1])
    m.top.navigate = openItem(c)
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if not press then return false
    if key = "down" and m.buttons.isInFocusChain() and m.children.visible
        m.onChildren = true
        m.children.setFocus(true)
        return true
    end if
    if key = "up" and m.children.isInFocusChain()
        m.onChildren = false
        m.buttons.setFocus(true)
        return true
    end if
    return false
end function
