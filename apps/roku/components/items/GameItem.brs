sub init()
    m.tile = m.top.findNode("tile")
    m.stripe = m.top.findNode("stripe")
    m.league = m.top.findNode("league")
    m.status = m.top.findNode("status")
    m.away = m.top.findNode("away")
    m.awayScore = m.top.findNode("awayScore")
    m.home = m.top.findNode("home")
    m.homeScore = m.top.findNode("homeScore")
    m.foot = m.top.findNode("foot")
end sub

sub onContent()
    c = m.top.itemContent
    if c = invalid or c.game = invalid then return
    g = c.game
    state = getOr(g, "state", "pre")
    shown = scoreShown(g)
    m.stripe.color = sportColor(c.sport)
    m.league.text = c.leagueName
    status = gameStatus(g)
    if state = "in"
        if shown then status = "LIVE  " + status else status = "LIVE"
        m.status.color = "0x2DD4BFFF"
    else
        if state = "post" and not shown then status = "Final"
        m.status.color = "0x98A1ADFF"
    end if
    m.status.text = status
    white = "0xF2F4F7FF"
    muted = "0x98A1ADFF"
    if isRace(g)
        ' A race: its name, then the top three once it's going.
        m.away.text = getOr(g, "shortName", getOr(g, "name", ""))
        m.away.color = white
        m.awayScore.text = ""
        top = []
        if shown
            for each d in getOr(g, "race", [])
                if top.Count() < 3 then top.Push(asString(d.place) + ". " + getOr(d, "shortName", getOr(d, "name", "")))
            end for
        end if
        m.home.text = joinStrings(top, "   ")
        m.home.color = muted
        m.homeScore.text = ""
    else
        m.away.text = sideName(g.away)
        m.home.text = sideName(g.home)
        if shown
            m.awayScore.text = getOr(g.away, "score", "")
            m.homeScore.text = getOr(g.home, "score", "")
        else if state = "pre"
            m.awayScore.text = getOr(g.away, "record", "")
            m.homeScore.text = getOr(g.home, "record", "")
        else
            m.awayScore.text = "-"
            m.homeScore.text = "-"
        end if
        ' The loser of a finished game is greyed out, once the score shows.
        m.away.color = white
        m.home.color = white
        if state = "post" and shown
            if isTrue(g.home.winner) then m.away.color = muted
            if isTrue(g.away.winner) then m.home.color = muted
        end if
        m.awayScore.color = m.away.color
        m.homeScore.color = m.home.color
        if state = "pre"
            m.awayScore.color = muted
            m.homeScore.color = muted
        end if
    end if
    foot = ""
    chans = getOr(g, "channels", [])
    if chans.Count() > 0
        foot = "On " + joinStrings([asString(chans[0].number), getOr(chans[0], "name", "")], " ")
    else if getOr(g, "broadcasts", []).Count() > 0 and state <> "post"
        foot = "TV: " + joinStrings(g.broadcasts, ", ")
    end if
    if isTrue(c.mine) then foot = joinStrings(["Your team", foot], "  ·  ")
    m.foot.text = foot
    layout()
end sub

sub layout()
    w = m.top.width
    h = m.top.height
    if w = 0 or h = 0 then return
    m.tile.width = w
    m.tile.height = h
    m.stripe.height = h
    pad = 26
    inner = w - pad - 18
    m.league.width = inner * 0.55
    m.league.translation = [pad, 14]
    m.status.width = inner * 0.6
    m.status.translation = [pad + inner * 0.4, 14]
    m.away.width = inner - 110
    m.away.translation = [pad, 50]
    m.awayScore.width = 120
    m.awayScore.translation = [pad + inner - 120, 48]
    m.home.width = inner - 110
    m.home.translation = [pad, 92]
    m.homeScore.width = 120
    m.homeScore.translation = [pad + inner - 120, 90]
    m.foot.width = inner
    m.foot.translation = [pad, h - 38]
end sub

sub onFocus()
end sub
