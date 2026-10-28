' Finds BingeBandit servers on the home network by asking every address in
' the Roku's /24 (e.g. 192.168.1.1-254) on port 8484, a batch at a time.

sub init()
    m.top.functionName = "scan"
end sub

sub scan()
    prefix = ""
    for each name in CreateObject("roDeviceInfo").GetIPAddrs()
        ip = CreateObject("roDeviceInfo").GetIPAddrs()[name]
        parts = ip.Split(".")
        if parts.Count() = 4 then prefix = parts[0] + "." + parts[1] + "." + parts[2] + "."
        if prefix <> "" then exit for
    end for
    if prefix = "" then
        m.top.done = true
        return
    end if
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
        deadline = CreateObject("roTimespan")
        while pending.Count() > 0 and deadline.TotalMilliseconds() < 1200
            msg = wait(1200 - deadline.TotalMilliseconds(), port)
            if type(msg) = "roUrlEvent"
                key = msg.GetSourceIdentity().ToStr()
                p = pending[key]
                if p <> invalid
                    pending.Delete(key)
                    if msg.GetResponseCode() = 200
                        data = ParseJson(msg.GetString())
                        if type(data) = "roAssociativeArray" and (getOr(data, "product", "") = "BingeBandit" or getOr(data, "status", "") = "ok")
                            name = getOr(data, "name", "")
                            if name = "" then name = "BingeBandit"
                            m.top.found = { url: p.url, name: name, version: getOr(data, "version", ""), id: getOr(data, "id", "") }
                        end if
                    end if
                end if
            end if
        end while
        for each key in pending
            pending[key].xfer.AsyncCancel()
        end for
    end for
    m.top.done = true
end sub
