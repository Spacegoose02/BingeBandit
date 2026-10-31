' Audio and subtitle choice, ported from the Android TV app (apps/tv/src/tracks.ts).

function textSubCodecs() as object
    return { subrip: true, srt: true, ass: true, ssa: true, webvtt: true, mov_text: true, text: true }
end function

function imageSubCodecs() as object
    return { hdmv_pgs_subtitle: true, dvd_subtitle: true, dvb_subtitle: true, xsub: true }
end function

function isTextSub(s as dynamic) as boolean
    if s = invalid then return false
    return textSubCodecs().DoesExist(LCase(getOr(s, "codec", "")))
end function

function isImageSub(s as dynamic) as boolean
    if s = invalid then return false
    return imageSubCodecs().DoesExist(LCase(getOr(s, "codec", "")))
end function

' ISO 639-2 code, its aliases and English name.
function languageTable() as object
    return [
        ["eng", ["en", "english"], "English"], ["spa", ["es", "spanish"], "Spanish"], ["fre", ["fr", "fra", "french"], "French"],
        ["ger", ["de", "deu", "german"], "German"], ["ita", ["it", "italian"], "Italian"], ["por", ["pt", "portuguese"], "Portuguese"],
        ["dut", ["nl", "nld", "dutch"], "Dutch"], ["swe", ["sv", "swedish"], "Swedish"], ["nor", ["no", "nb", "nob", "norwegian"], "Norwegian"],
        ["dan", ["da", "danish"], "Danish"], ["fin", ["fi", "finnish"], "Finnish"], ["pol", ["pl", "polish"], "Polish"],
        ["rus", ["ru", "russian"], "Russian"], ["ukr", ["uk", "ukrainian"], "Ukrainian"], ["cze", ["cs", "ces", "czech"], "Czech"],
        ["gre", ["el", "ell", "greek"], "Greek"], ["tur", ["tr", "turkish"], "Turkish"], ["ara", ["ar", "arabic"], "Arabic"],
        ["heb", ["he", "hebrew"], "Hebrew"], ["hin", ["hi", "hindi"], "Hindi"], ["jpn", ["ja", "japanese"], "Japanese"],
        ["kor", ["ko", "korean"], "Korean"], ["chi", ["zh", "zho", "chinese"], "Chinese"], ["tha", ["th", "thai"], "Thai"],
        ["vie", ["vi", "vietnamese"], "Vietnamese"], ["ind", ["id", "indonesian"], "Indonesian"], ["hun", ["hu", "hungarian"], "Hungarian"],
        ["rum", ["ro", "ron", "romanian"], "Romanian"]
    ]
end function

function normLang(code as dynamic) as string
    c = LCase(asString(code))
    if c = "" then return ""
    for each row in languageTable()
        if row[0] = c then return c
        for each a in row[1]
            if a = c then return row[0]
        end for
    end for
    return c
end function

function languageName(code as dynamic) as string
    c = normLang(code)
    if c = "" or c = "und" then return "Unknown"
    for each row in languageTable()
        if row[0] = c then return row[2]
    end for
    return asString(code)
end function

function channelLabel(n as dynamic) as string
    if n = invalid or n = 0 then return ""
    if n = 1 then return "Mono"
    if n = 2 then return "Stereo"
    if n = 6 then return "5.1"
    if n = 8 then return "7.1"
    return n.ToStr() + "ch"
end function

function streamLabel(s as object) as string
    bits = [languageName(getOr(s, "language", ""))]
    if s.type = "audio" then bits.Push((UCase(getOr(s, "codec", "")) + " " + channelLabel(getOr(s, "channels", 0))).Trim())
    bits.Push(getOr(s, "title", ""))
    if isTrue(s.forced) then bits.Push("Forced")
    if isTrue(s.hearingImpaired) then bits.Push("SDH")
    return joinStrings(bits, " · ")
end function

function streamsOf(f as object, kind as string) as object
    out = []
    for each s in getOr(f, "streams", [])
        if s.type = kind
            if kind <> "subtitle" or isTextSub(s) or isImageSub(s) then out.Push(s)
        end if
    end for
    return out
end function

' The file's own default audio track (the first one when none is marked).
function fileDefaultAudio(f as object) as dynamic
    audios = streamsOf(f, "audio")
    for each s in audios
        if isTrue(s.default) then return s
    end for
    if audios.Count() > 0 then return audios[0]
    return invalid
end function

' The audio stream playing for a choice: -1 is the file's default.
function audioFor(f as object, index as integer) as dynamic
    if index >= 0
        for each s in streamsOf(f, "audio")
            if s.index = index then return s
        end for
    end if
    return fileDefaultAudio(f)
end function

' Text over image subtitles, plain over SDH.
function bestSub(list as object) as dynamic
    best = invalid
    bestRank = 99
    for each s in list
        r = 0
        if isImageSub(s) then r = r + 2
        if isTrue(s.hearingImpaired) then r = r + 1
        if r < bestRank
            best = s
            bestRank = r
        end if
    end for
    return best
end function

' Picks audio and subtitles from the viewer's preferences: the audio stream
' (-1 when it's the file default, so direct play stays possible) and the
' subtitle stream to show, if any.
function pickTracks(f as object, p as dynamic) as object
    audios = streamsOf(f, "audio")
    def = fileDefaultAudio(f)
    chosen = def
    if p <> invalid and getOr(p, "audioLanguage", "") <> ""
        want = normLang(p.audioLanguage)
        for each s in audios
            if normLang(getOr(s, "language", "")) = want
                chosen = s
                exit for
            end if
        end for
    end if
    audio = -1
    if chosen <> invalid and def <> invalid and chosen.index <> def.index then audio = chosen.index
    if p = invalid then return { audio: audio, subtitle: invalid }

    want = normLang(getOr(p, "subtitleLanguage", ""))
    inLangFull = []
    inLangForced = []
    noLang = []
    for each s in streamsOf(f, "subtitle")
        lang = normLang(getOr(s, "language", ""))
        if lang = want
            if isTrue(s.forced) then inLangForced.Push(s) else inLangFull.Push(s)
        else if lang = "" and not isTrue(s.forced)
            noLang.Push(s)
        end if
    end for
    full = bestSub(inLangFull)
    if full = invalid then full = bestSub(noLang)
    forced = bestSub(inLangForced)
    foreignAudio = chosen <> invalid and getOr(chosen, "language", "") <> "" and normLang(chosen.language) <> want

    sub_ = invalid
    mode = getOr(p, "subtitleMode", "auto")
    if mode = "always"
        sub_ = full
        if sub_ = invalid then sub_ = forced
    else if mode = "auto"
        if foreignAudio then sub_ = full
        if sub_ = invalid then sub_ = forced
    else if mode = "forced"
        sub_ = forced
    end if
    return { audio: audio, subtitle: sub_ }
end function

' --- WebVTT ----------------------------------------------------------------------

function vttSeconds(t as string) as float
    p = t.Trim().Split(":")
    if p.Count() = 3 then return Val(p[0]) * 3600 + Val(p[1]) * 60 + Val(p[2])
    if p.Count() = 2 then return Val(p[0]) * 60 + Val(p[1])
    return Val(t)
end function

' Cues as { s, e, lines } sorted by start, tags stripped.
function parseVtt(text as string) as object
    cues = []
    tags = CreateObject("roRegex", "<[^>]+>", "")
    lines = text.Replace(Chr(13), "").Split(Chr(10))
    i = 0
    n = lines.Count()
    while i < n
        line = lines[i]
        arrow = Instr(1, line, "-->")
        if arrow > 0
            a = Left(line, arrow - 1)
            b = Mid(line, arrow + 3).Trim()
            sp = Instr(1, b, " ")
            if sp > 0 then b = Left(b, sp - 1)
            body = []
            i = i + 1
            while i < n and lines[i].Trim() <> ""
                l = tags.ReplaceAll(lines[i], "").Trim()
                if l <> "" then body.Push(l)
                i = i + 1
            end while
            if body.Count() > 0 then cues.Push({ s: vttSeconds(a), e: vttSeconds(b), lines: body })
        else
            i = i + 1
        end if
    end while
    return cues
end function
