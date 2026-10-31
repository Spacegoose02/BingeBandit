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
    if build = "learn"
        learnAddresses()
        m.top.response = { ok: true, status: 200 }
        return
    end if
    if build = "vtt"
        m.top.response = loadVtt(req)
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
        else if build = "guide"
            content = guideContent(res, getOr(req, "filter", "all"))
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
    unreachable = { ok: false, status: 0, error: "Can't reach " + host + ". Is the server on?" }
    if type(msg) <> "roUrlEvent"
        xfer.AsyncCancel()
        ' Timed out: it may have landed, so only a read is sent again.
        if useToken and method = "GET" and getOr(req, "retried", false) <> true and reconnect(server) then return httpRequest(retryOf(req))
        return unreachable
    end if
    code = msg.GetResponseCode()
    if code <= 0
        if useToken and getOr(req, "retried", false) <> true and reconnect(server) then return httpRequest(retryOf(req))
        return unreachable
    end if
    body = msg.GetString()
    data = invalid
    if getOr(req, "raw", false) = true
        data = body
    else if body <> invalid and body <> ""
        data = ParseJson(body)
    end if
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
    return { url: url, name: name, version: getOr(res.data, "version", ""), id: getOr(res.data, "id", "") }
end function

' --- Other addresses of the signed-in server ---------------------------------

' A Roku signed in with the public URL can't reach the server when the
' internet is down, even on the same network. The channel learns the server's
' LAN address and public URL while it works, and switches when the address it
' uses stops answering. The id from /api/health keeps it to the same server.

function retryOf(req as object) as object
    r = {}
    r.Append(req)
    r.retried = true
    return r
end function

function knownAddresses() as object
    out = [m.global.server]
    for each a in m.global.alternates
        out.Push(a)
    end for
    return out
end function

' Uses `url` from now on, keeping the other addresses, and saves the session.
sub switchTo(url as string)
    others = []
    for each a in knownAddresses()
        if a <> url then others.Push(a)
    end for
    s = currentSession()
    s.server = url
    s.alternates = others
    saveSession(s)
end sub

sub rememberAddress(url as string)
    for each a in knownAddresses()
        if a = url then return
    end for
    s = currentSession()
    alts = []
    alts.Append(m.global.alternates)
    alts.Push(url)
    s.alternates = alts
    saveSession(s)
end sub

function sameServer(url as string, timeout as integer) as boolean
    info = probe(url, timeout)
    if info = invalid then return false
    id = m.global.serverId
    return id = "" or info.id = "" or info.id = id
end function

' After `failed` didn't answer: true when another address of the server
' answers (now m.global.server). Requests failing together wait for the
' first one's look around instead of each starting their own.
function reconnect(failed as string) as boolean
    if m.global.server <> failed then return true
    if m.global.reconnecting
        waited = CreateObject("roTimespan")
        while m.global.reconnecting and waited.TotalMilliseconds() < 20000
            sleep(250)
        end while
        return m.global.server <> failed
    end if
    m.global.reconnecting = true
    found = ""
    for each a in knownAddresses()
        if a <> failed and sameServer(a, 3000)
            found = a
            exit for
        end if
    end for
    if found = "" and m.global.serverId <> "" then found = findOnLan(m.global.serverId)
    if found <> "" then switchTo(found)
    m.global.reconnecting = false
    return found <> ""
end function

function isLocalAddress(url as string) as boolean
    return CreateObject("roRegex", "^https?://(10\.|127\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|[^/:]+\.local([:/]|$))", "i").IsMatch(url)
end function

' The server on the Roku's network, by its id: every address in its /24 on
' port 8484, a batch at a time (like ScanTask).
function findOnLan(id as string) as string
    sec = CreateObject("roRegistrySection", "bingebandit")
    sec.Write("lanScanAt." + id, CreateObject("roDateTime").AsSeconds().ToStr())
    sec.Flush()
    prefix = ""
    ips = CreateObject("roDeviceInfo").GetIPAddrs()
    for each name in ips
        parts = ips[name].Split(".")
        if parts.Count() = 4 then prefix = parts[0] + "." + parts[1] + "." + parts[2] + "."
        if prefix <> "" then exit for
    end for
    if prefix = "" then return ""
    batch = 30
    for start = 1 to 254 step batch
        port = CreateObject("roMessagePort")
        pending = {}
        for host = start to start + batch - 1
            if host > 254 then exit for
            url = "http://" + prefix + host.ToStr() + ":8484"
            x = CreateObject("roUrlTransfer")
            x.SetMessagePort(port)
            x.SetUrl(url + "/api/health")
            if x.AsyncGetToString() then pending[x.GetIdentity().ToStr()] = { xfer: x, url: url }
        end for
        found = ""
        deadline = CreateObject("roTimespan")
        while pending.Count() > 0 and deadline.TotalMilliseconds() < 1200 and found = ""
            msg = wait(1200 - deadline.TotalMilliseconds(), port)
            if type(msg) = "roUrlEvent"
                key = msg.GetSourceIdentity().ToStr()
                p = pending[key]
                if p <> invalid
                    pending.Delete(key)
                    if msg.GetResponseCode() = 200
                        data = ParseJson(msg.GetString())
                        if type(data) = "roAssociativeArray" and getOr(data, "id", "") = id then found = p.url
                    end if
                end if
            end if
        end while
        for each key in pending
            pending[key].xfer.AsyncCancel()
        end for
        if found <> "" then return found
    end for
    return ""
end function

' While the server answers: note its id and public URL, and when only
' public addresses are known, look for it on this network (once a day).
sub learnAddresses()
    info = probe(m.global.server, 4000)
    if info = invalid then return
    if info.id <> "" and info.id <> m.global.serverId
        s = currentSession()
        s.serverId = info.id
        saveSession(s)
    end if
    res = httpRequest({ path: "/api/link/info", retried: true })
    if res.ok and type(res.data) = "roAssociativeArray"
        pub = asString(res.data.serverUrl)
        while Right(pub, 1) = "/"
            pub = Left(pub, Len(pub) - 1)
        end while
        if LCase(Left(pub, 4)) = "http" then rememberAddress(pub)
    end if
    id = m.global.serverId
    if id = "" then return
    for each a in knownAddresses()
        if isLocalAddress(a) then return
    end for
    sec = CreateObject("roRegistrySection", "bingebandit")
    last = 0
    if sec.Exists("lanScanAt." + id) then last = sec.Read("lanScanAt." + id).ToInt()
    if CreateObject("roDateTime").AsSeconds() - last < 86400 then return
    lan = findOnLan(id)
    if lan <> "" then rememberAddress(lan)
end sub

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

' Subtitle cues for the player, parsed here so a long film's file doesn't
' stall the render thread. Embedded streams are pulled out of the video by
' the server the first time, which can take a while.
function loadVtt(req as object) as object
    r = {}
    r.Append(req)
    r.raw = true
    r.timeout = 90000
    res = httpRequest(r)
    if not res.ok then return res
    return { ok: true, status: res.status, data: parseVtt(asString(res.data)) }
end function

' --- Live TV guide -------------------------------------------------------------

' The guide's channel filters: all, favourites, the viewer's own categories
' ("my:<id>") and the provider's groups.
function guideFilters(data as object) as object
    chans = getOr(data, "channels", [])
    out = [{ key: "all", label: "All channels" }]
    for each c in chans
        if isTrue(c.favorite)
            out.Push({ key: "fav", label: "Favourites" })
            exit for
        end if
    end for
    for each cat in getOr(data, "categories", [])
        out.Push({ key: "my:" + asString(cat.id), label: getOr(cat, "name", "") })
    end for
    seen = {}
    for each c in chans
        g = getOr(c, "group", "")
        if g <> "" and not seen.DoesExist(g)
            seen[g] = true
            out.Push({ key: "group:" + g, label: g })
        end if
    end for
    return out
end function

function filterChannels(data as object, filter as string) as object
    chans = getOr(data, "channels", [])
    if filter = "all" then return chans
    out = []
    if Left(filter, 3) = "my:"
        ids = {}
        for each cat in getOr(data, "categories", [])
            if "my:" + asString(cat.id) = filter
                for each id in getOr(cat, "channelIds", [])
                    ids[asString(id)] = true
                end for
            end if
        end for
        for each c in chans
            if ids.DoesExist(asString(c.id)) then out.Push(c)
        end for
        return out
    end if
    for each c in chans
        if filter = "fav" and isTrue(c.favorite) then out.Push(c)
        if Left(filter, 6) = "group:" and "group:" + getOr(c, "group", "") = filter then out.Push(c)
    end for
    return out
end function

' One TimeGrid row per channel, its programs between from and to with the
' gaps filled ("No guide information"), so the grid's program indexes are
' the same on every firmware. Adds `shown` (the channels in grid order) and
' `filters` to the response.
function guideContent(res as object, filter as string) as object
    data = res.data
    root = CreateObject("roSGNode", "ContentNode")
    if type(data) <> "roAssociativeArray" then return root
    filters = guideFilters(data)
    known = false
    for each f in filters
        if f.key = filter then known = true
    end for
    ' A filter that went away (a deleted category, no favourites left) falls back to all.
    if not known then filter = "all"
    shown = filterChannels(data, filter)
    from = getOr(data, "from", 0)
    upto = getOr(data, "to", 0)
    progs = getOr(data, "programs", {})
    recs = getOr(data, "recordings", {})
    if type(recs) <> "roAssociativeArray" then recs = {}
    for each c in shown
        row = root.createChild("ContentNode")
        row.title = asString(c.number) + "   " + getOr(c, "name", "")
        row.addFields({ channelId: c.id })
        epg = getOr(c, "epgId", "")
        list = invalid
        if epg <> "" and type(progs) = "roAssociativeArray" then list = progs[epg]
        if type(list) <> "roArray" then list = []
        t = from
        for each p in list
            if p.stop > t and p.start < upto
                if p.start - t >= 60 then guideGap(row, t, p.start)
                start = p.start
                if start < from then start = from
                stopAt = p.stop
                if stopAt > upto then stopAt = upto
                n = row.createChild("ContentNode")
                title = getOr(p, "title", "")
                if recs.DoesExist(epg + ":" + asString(p.start)) then title = "REC  " + title
                n.title = title
                n.playStart = start
                n.playDuration = stopAt - start
                n.addFields({ program: p, gap: false })
                t = p.stop
            end if
        end for
        if upto - t >= 60 or row.getChildCount() = 0 then guideGap(row, t, upto)
    end for
    res.shown = shown
    res.filters = filters
    res.filter = filter
    return root
end function

sub guideGap(row as object, start as integer, stopAt as integer)
    n = row.createChild("ContentNode")
    n.title = "No guide information"
    n.playStart = start
    n.playDuration = stopAt - start
    n.addFields({ program: {}, gap: true })
end sub
