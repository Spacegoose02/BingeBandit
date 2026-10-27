' One request to the BingeBandit server, run off the render thread. With a
' `build` in the request it also turns the answer into ContentNodes here, so
' big libraries don't stall the UI.

sub init()
    m.top.functionName = "execute"
end sub

sub execute()
    req = m.top.request
    build = getOr(req, "build", "")
    if build = "probe"
        m.top.response = probeTyped(req.input)
        return
    end if
    if build = "library"
        m.top.response = loadLibrary(req)
        return
    end if
    res = httpRequest(req)
    if res.ok and build <> ""
        content = invalid
        if build = "home"
            content = homeContent(res.data)
        else if build = "items"
            content = gridContent(res.data, false)
        else if build = "wideItems"
            content = gridContent(res.data, true)
        else if build = "details"
            content = detailsContent(res.data)
        else if build = "channels"
            content = channelContent(res.data)
        end if
        m.top.content = content
    end if
    m.top.response = res
    if getOr(req, "detached", false) = true
        parent = m.top.getParent()
        if parent <> invalid then parent.removeChild(m.top)
    end if
end sub

' --- HTTP --------------------------------------------------------------------

function httpRequest(req as object) as object
    server = getOr(req, "server", m.global.server)
    if server = invalid or server = "" then return { ok: false, status: 0, error: "No server chosen" }
    url = server + req.path
    method = UCase(getOr(req, "method", "GET"))
    timeout = getOr(req, "timeout", 15000)
    useToken = getOr(req, "server", invalid) = invalid

    xfer = CreateObject("roUrlTransfer")
    port = CreateObject("roMessagePort")
    xfer.SetMessagePort(port)
    xfer.SetUrl(url)
    if LCase(Left(url, 6)) = "https:"
        xfer.SetCertificatesFile("common:/certs/ca-bundle.crt")
        xfer.InitClientCertificates()
    end if
    xfer.RetainBodyOnError(true)
    xfer.EnableEncodings(true)
    xfer.AddHeader("Accept", "application/json")
    token = getOr(req, "token", m.global.token)
    if useToken and token <> "" then xfer.AddHeader("Authorization", "Bearer " + token)

    if method = "GET"
        started = xfer.AsyncGetToString()
    else
        xfer.SetRequest(method)
        payload = ""
        if req.body <> invalid
            payload = FormatJson(req.body)
            xfer.AddHeader("Content-Type", "application/json")
        end if
        started = xfer.AsyncPostFromString(payload)
    end if
    if not started then return { ok: false, status: 0, error: "Couldn't start the request" }

    msg = wait(timeout, port)
    host = server.Replace("http://", "").Replace("https://", "")
    if type(msg) <> "roUrlEvent"
        xfer.AsyncCancel()
        return { ok: false, status: 0, error: "Can't reach " + host + ". Is the server on?" }
    end if
    code = msg.GetResponseCode()
    if code <= 0
        return { ok: false, status: 0, error: "Can't reach " + host + ". Is the server on?" }
    end if
    body = msg.GetString()
    data = invalid
    if body <> invalid and body <> "" then data = ParseJson(body)
    res = { ok: code >= 200 and code < 300, status: code, data: data }
    if not res.ok
        msgText = "Server error " + code.ToStr()
        if type(data) = "roAssociativeArray" and data.error <> invalid then msgText = data.error
        res.error = msgText
        ' A session that ended (signed out elsewhere, deleted user) sends
        ' everyone back to sign in.
        if code = 401 and useToken and token <> "" and token = m.global.token then m.global.unauthorized = true
    end if
    return res
end function

' --- Finding a server --------------------------------------------------------

' The addresses to try for what someone typed (mirrors the Android TV app): a
' bare name is usually a reverse proxy (https, no port), an IP is the server
' itself on :8484. Anything after the host is dropped, so a /link URL works.
function addressCandidates(input as string) as object
    s = input.Trim()
    scheme = ""
    lower = LCase(s)
    if Left(lower, 7) = "http://"
        scheme = "http://"
        s = Mid(s, 8)
    else if Left(lower, 8) = "https://"
        scheme = "https://"
        s = Mid(s, 9)
    end if
    for each c in ["/", "?", "#"]
        i = Instr(1, s, c)
        if i > 0 then s = Left(s, i - 1)
    end for
    if s = "" then return []
    if scheme = "http://"
        if CreateObject("roRegex", ":\d+$", "").IsMatch(s) then return ["http://" + s]
        return ["http://" + s + ":8484"]
    end if
    if scheme = "https://" then return ["https://" + s]
    if CreateObject("roRegex", ":\d+$", "").IsMatch(s) then return ["http://" + s, "https://" + s]
    if CreateObject("roRegex", "^\d+\.\d+\.\d+\.\d+$", "").IsMatch(s) then return ["http://" + s + ":8484", "http://" + s]
    return ["https://" + s, "http://" + s + ":8484", "http://" + s]
end function

function probeTyped(input as string) as object
    for each url in addressCandidates(input)
        info = probe(url, 4000)
        if info <> invalid then return { ok: true, status: 200, data: info }
    end for
    return { ok: false, status: 0, error: "No BingeBandit server answered at " + input.Trim() + ". Check the address, and that the server is running." }
end function

function probe(url as string, timeout as integer) as dynamic
    res = httpRequest({ server: url, path: "/api/health", timeout: timeout })
    if not res.ok or type(res.data) <> "roAssociativeArray" then return invalid
    if getOr(res.data, "product", "") <> "BingeBandit" and getOr(res.data, "status", "") <> "ok" then return invalid
    name = getOr(res.data, "name", "")
    if name = "" then name = "BingeBandit"
    return { url: url, name: name, version: getOr(res.data, "version", "") }
end function

' --- Content builders --------------------------------------------------------

' One poster or landscape tile. Episodes show their show's artwork as a poster
' and their own still when wide.
function itemNode(it as object, wide as boolean, inShow = false as boolean) as object
    n = CreateObject("roSGNode", "ContentNode")
    kind = getOr(it, "kind", "movie")
    title = getOr(it, "title", "")
    subtitle = ""
    if kind = "episode"
        code = episodeCode(it)
        if getOr(it, "showTitle", "") <> "" and not inShow
            subtitle = joinStrings([code, title], " · ")
            title = it.showTitle
        else
            subtitle = code
        end if
    else if kind = "season"
        if it.childCount <> invalid then subtitle = it.childCount.ToStr() + " episodes"
    else if it.year <> invalid
        subtitle = it.year.ToStr()
    end if
    if wide
        art = getOr(it, "backdrop", "")
        if art = "" then art = getOr(it, "showBackdrop", "")
        if art = "" then art = getOr(it, "poster", "")
        poster = imageUrl(art, "w780")
    else
        art = getOr(it, "poster", "")
        if art = "" then art = getOr(it, "showPoster", "")
        poster = imageUrl(art, "w342")
    end if
    progress = 0.0
    watched = false
    w = it.watch
    if w <> invalid
        watched = isTrue(w.watched)
        if not watched and w.duration <> invalid and w.duration > 0 and w.position <> invalid and w.position > 0
            progress = w.position / w.duration
        end if
    end if
    n.addFields({ itemId: it.id, kind: kind, subtitle: subtitle, poster: poster, wide: wide, progress: progress, watched: watched, unwatched: getOr(it, "unwatchedCount", 0), backdrop: imageUrl(firstOf(it, ["backdrop", "showBackdrop"]), "w1280") })
    n.title = title
    return n
end function

function collectionNode(c as object) as object
    n = CreateObject("roSGNode", "ContentNode")
    count = getOr(c, "itemCount", 0)
    n.addFields({ itemId: c.id, kind: "collection", subtitle: count.ToStr() + " titles", poster: imageUrl(getOr(c, "poster", ""), "w342"), wide: false, progress: 0.0, watched: false, unwatched: 0, backdrop: imageUrl(getOr(c, "backdrop", ""), "w1280") })
    n.title = getOr(c, "name", "")
    return n
end function

' Home: Continue Watching (landscape) and then the server's rows.
function homeContent(data as dynamic) as object
    root = CreateObject("roSGNode", "ContentNode")
    if data = invalid then return root
    cw = getOr(data, "continueWatching", [])
    if cw.Count() > 0
        row = root.createChild("ContentNode")
        row.title = "Continue Watching"
        row.addFields({ wide: true })
        for each it in cw
            row.appendChild(itemNode(it, true))
        end for
    end if
    for each r in getOr(data, "rows", [])
        items = getOr(r, "items", [])
        if items.Count() > 0
            row = root.createChild("ContentNode")
            row.title = getOr(r, "title", "")
            row.addFields({ wide: false })
            for each it in items
                row.appendChild(itemNode(it, false))
            end for
        end if
    end for
    return root
end function

' A grid of titles from a list, or from {items: [...]} (collections, search).
function gridContent(data as dynamic, wide as boolean) as object
    root = CreateObject("roSGNode", "ContentNode")
    items = data
    if type(data) = "roAssociativeArray" then items = getOr(data, "items", [])
    if type(items) <> "roArray" then return root
    for each it in items
        root.appendChild(itemNode(it, wide))
    end for
    return root
end function

' A whole library sorted by title, collections mixed in by their sort title
' like the web's library page. Pages of 500 until the total.
function loadLibrary(req as object) as object
    items = []
    cols = []
    total = -1
    while total < 0 or items.Count() < total
        res = httpRequest({ path: req.path + "?sort=title&offset=" + items.Count().ToStr() + "&limit=500" })
        if not res.ok then return res
        page = getOr(res.data, "items", [])
        if items.Count() = 0 then cols = getOr(res.data, "collections", [])
        total = getOr(res.data, "total", 0)
        items.Append(page)
        if page.Count() = 0 then exit while
    end while
    root = CreateObject("roSGNode", "ContentNode")
    ci = 0
    for each it in items
        key = LCase(getOr(it, "sortTitle", getOr(it, "title", "")))
        while ci < cols.Count() and LCase(getOr(cols[ci], "sortTitle", getOr(cols[ci], "name", ""))) <= key
            root.appendChild(collectionNode(cols[ci]))
            ci = ci + 1
        end while
        root.appendChild(itemNode(it, false))
    end for
    while ci < cols.Count()
        root.appendChild(collectionNode(cols[ci]))
        ci = ci + 1
    end while
    m.top.content = root
    return { ok: true, status: 200, data: { total: total } }
end function

' The children under an item as one RowList row: a show's seasons
' (posters) or a season's episodes (stills).
function detailsContent(data as object) as object
    root = CreateObject("roSGNode", "ContentNode")
    kids = getOr(data, "children", [])
    if kids.Count() = 0 then return root
    row = root.createChild("ContentNode")
    if getOr(data.item, "kind", "") = "show" then row.title = "Seasons" else row.title = "Episodes"
    for each it in kids
        row.appendChild(itemNode(it, getOr(it, "kind", "") = "episode", true))
    end for
    return root
end function

function channelContent(data as dynamic) as object
    root = CreateObject("roSGNode", "ContentNode")
    if type(data) <> "roArray" then return root
    for each c in data
        n = root.createChild("ContentNode")
        now = getOr(c, "now", invalid)
        nxt = getOr(c, "next", invalid)
        nowText = ""
        if now <> invalid then nowText = getOr(now, "title", "")
        nextText = ""
        if nxt <> invalid then nextText = "Next " + wallClock(nxt.start) + "  " + getOr(nxt, "title", "")
        logo = ""
        if getOr(c, "logo", "") <> "" then logo = withToken(absoluteUrl(c.logo))
        progress = 0.0
        if now <> invalid and now.stop > now.start
            nowSecs = CreateObject("roDateTime").AsSeconds()
            progress = (nowSecs - now.start) / (now.stop - now.start)
        end if
        n.addFields({ channelId: c.id, number: asString(c.number), logo: logo, nowTitle: nowText, nextTitle: nextText, progress: progress, favorite: getOr(c, "favorite", false) })
        n.title = getOr(c, "name", "")
    end for
    return root
end function
