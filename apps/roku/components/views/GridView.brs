sub init()
    m.grid = m.top.findNode("grid")
    m.status = m.top.findNode("status")
    m.grid.observeField("itemSelected", "onSelected")
    m.grid.observeField("itemFocused", "onFocused")
    m.top.observeField("params", "onParams")
    m.top.observeField("refresh", "load")
    m.top.observeField("focusedChild", "onFocusChanged")
    m.filters = { genre: "", decade: 0, rating: 0, unwatched: false }
    m.filterOptions = invalid
    ' A collection's order: "" is the collection's own, else one picked with *.
    m.sort = ""
    m.shownSort = ""
    m.hasList = false
end sub

sub onFocusChanged()
    if m.top.hasFocus() then m.grid.setFocus(true)
end sub

' params: { title, path, build: items|library, columns? }
sub onParams()
    p = m.top.params
    m.top.findNode("title").text = getOr(p, "title", "")
    cols = getOr(p, "columns", 7)
    m.grid.numColumns = cols
    m.status.text = "Loading..."
    load()
end sub

sub load()
    p = m.top.params
    if p = invalid or p.path = invalid then return
    path = p.path
    if isCollection() and m.sort <> "" then path = path + "?sort=" + m.sort
    api("GET", path, invalid, "onLoaded", { build: getOr(p, "build", "items"), query: filterQuery() })
end sub

sub onLoaded(event as object)
    t = event.getRoSGNode()
    doneTask(t)
    res = event.getData()
    if not res.ok
        if m.grid.content = invalid then m.status.text = res.error
        return
    end if
    content = t.content
    n = content.getChildCount()
    filtered = filterQuery() <> ""
    if n = 0
        if filtered then m.status.text = "Nothing here matches these filters. * to change them." else m.status.text = getOr(m.top.params, "empty", "Nothing here yet.")
        m.top.findNode("count").text = filterSummary()
    else
        m.status.text = ""
        if n = 1 then count = "1 title" else count = n.ToStr() + " titles"
        if isLibrary() then count = joinStrings([count, filterSummary(), "* to filter"], "   ·   ")
        if isCollection()
            data = res.data
            if type(data) = "roAssociativeArray"
                m.shownSort = getOr(data, "sort", "")
                if m.shownSort = "" then m.shownSort = "release"
                col = getOr(data, "collection", {})
                m.hasList = getOr(col, "listUrl", "") <> ""
            end if
            if n > 1 then count = joinStrings([count, "Sorted by " + sortLabel(m.shownSort), "* to sort"], "   ·   ")
        end if
        m.top.findNode("count").text = count
    end if
    focus = m.grid.itemFocused
    m.grid.content = content
    if focus > 0 and focus < n then m.grid.jumpToItem = focus
end sub

sub onFocused()
    if m.grid.content = invalid then return
    c = m.grid.content.getChild(m.grid.itemFocused)
    if c <> invalid then m.top.backdrop = c.backdrop
end sub

sub onSelected()
    if m.grid.content = invalid then return
    c = m.grid.content.getChild(m.grid.itemSelected)
    if c <> invalid then m.top.navigate = openItem(c)
end sub

' --- Collection sort (the * button) ------------------------------------------

function isCollection() as boolean
    return m.top.params <> invalid and Left(getOr(m.top.params, "path", ""), 17) = "/api/collections/"
end function

' The orders a viewer can pick, as the server names them, like the Android TV
' and web apps. "release" is the default oldest-first order.
function collectionSorts() as object
    sorts = [
        { value: "release", label: "Release date (oldest first)" },
        { value: "release_desc", label: "Release date (newest first)" },
        { value: "title", label: "Title" },
        { value: "added", label: "Recently added" },
        { value: "rating", label: "Rating" },
    ]
    if m.hasList or m.shownSort = "list" then sorts.Push({ value: "list", label: "List order" })
    return sorts
end function

function sortLabel(value as string) as string
    for each s in collectionSorts()
        if s.value = value then return s.label
    end for
    return "Release date (oldest first)"
end function

sub openSort()
    m.sortChoices = collectionSorts()
    buttons = []
    for each s in m.sortChoices
        label = s.label
        if s.value = m.shownSort then label = label + "  (current)"
        buttons.Push(label)
    end for
    showDialog("Sort by", buttons, "sort")
end sub

' --- Library filters (the * button) ------------------------------------------

function isLibrary() as boolean
    return m.top.params <> invalid and getOr(m.top.params, "build", "") = "library"
end function

' The filters as items query parameters, like the web and Android TV apps.
function filterQuery() as string
    if not isLibrary() then return ""
    f = m.filters
    q = ""
    if f.genre <> "" then q = q + "&genre=" + f.genre.EncodeUriComponent()
    if f.decade > 0 then q = q + "&yearFrom=" + f.decade.ToStr() + "&yearTo=" + (f.decade + 9).ToStr()
    if f.rating > 0 then q = q + "&minRating=" + f.rating.ToStr()
    if f.unwatched then q = q + "&unwatched=1"
    return q
end function

function filterSummary() as string
    f = m.filters
    parts = [f.genre]
    if f.decade > 0 then parts.Push(f.decade.ToStr() + "s")
    if f.rating > 0 then parts.Push("Rated " + f.rating.ToStr() + "+")
    if f.unwatched then parts.Push("Unwatched")
    return joinStrings(parts, ", ")
end function

sub openFilters()
    if m.filterOptions = invalid
        api("GET", m.top.params.path.Replace("/items", "/filters"), invalid, "onFilterOptions")
        return
    end if
    f = m.filters
    genre = "All"
    if f.genre <> "" then genre = f.genre
    year = "Any"
    if f.decade > 0 then year = f.decade.ToStr() + "s"
    rating = "Any"
    if f.rating > 0 then rating = f.rating.ToStr() + "+"
    unwatched = "Off"
    if f.unwatched then unwatched = "On"
    showDialog("Filter", ["Genre: " + genre, "Year: " + year, "Rating: " + rating, "Unwatched only: " + unwatched, "Clear filters"], "menu")
end sub

sub onFilterOptions(event as object)
    doneTask(event.getRoSGNode())
    res = event.getData()
    if not res.ok
        m.status.text = res.error
        return
    end if
    m.filterOptions = res.data
    openFilters()
end sub

sub showDialog(title as string, buttons as object, kind as string)
    d = CreateObject("roSGNode", "StandardMessageDialog")
    d.title = title
    d.buttons = buttons
    d.observeField("buttonSelected", "onDialogButton")
    d.observeField("wasClosed", "onDialogClosed")
    m.dialog = d
    m.dialogKind = kind
    m.top.getScene().dialog = d
end sub

sub onDialogClosed()
    if m.dialog <> invalid and m.dialog.wasClosed then m.dialog = invalid
end sub

sub onDialogButton()
    if m.dialog = invalid then return
    i = m.dialog.buttonSelected
    kind = m.dialogKind
    m.dialog.close = true
    m.dialog = invalid
    if kind = "sort"
        m.sort = m.sortChoices[i].value
        applyFilters()
        return
    end if
    opts = m.filterOptions
    if kind = "menu"
        if i = 0
            genres = ["All genres"]
            genres.Append(getOr(opts, "genres", []))
            showDialog("Genre", genres, "genre")
        else if i = 1
            years = ["Any year"]
            for each decade in getOr(opts, "decades", [])
                years.Push(decade.ToStr() + "s")
            end for
            showDialog("Year", years, "decade")
        else if i = 2
            showDialog("Rating", ["Any rating", "Rated 6+", "Rated 7+", "Rated 8+"], "rating")
        else if i = 3
            m.filters.unwatched = not m.filters.unwatched
            applyFilters()
        else
            m.filters = { genre: "", decade: 0, rating: 0, unwatched: false }
            applyFilters()
        end if
        return
    end if
    if kind = "genre"
        if i = 0 then m.filters.genre = "" else m.filters.genre = opts.genres[i - 1]
    else if kind = "decade"
        if i = 0 then m.filters.decade = 0 else m.filters.decade = opts.decades[i - 1]
    else if kind = "rating"
        if i = 0 then m.filters.rating = 0 else m.filters.rating = 5 + i
    end if
    applyFilters()
end sub

sub applyFilters()
    m.grid.content = invalid
    m.top.findNode("count").text = ""
    m.status.text = "Loading..."
    load()
end sub

function onKeyEvent(key as string, press as boolean) as boolean
    if press and key = "options" and isLibrary()
        openFilters()
        return true
    end if
    if press and key = "options" and isCollection() and m.grid.content <> invalid and m.grid.content.getChildCount() > 1
        openSort()
        return true
    end if
    return false
end function
