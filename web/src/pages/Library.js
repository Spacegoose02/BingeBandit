import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { CollectionCard, PosterCard } from '../components/Cards';
import { CollectionEditor } from '../components/CollectionEditor';
import { Icon } from '../components/Icon';
import { RecommendationsEditor } from '../components/RecommendationsEditor';
import { Row } from '../components/Row';
import { useApi, useDocumentTitle } from '../hooks';
const PAGE = 120;
const sorts = [
    ['title', 'Title'],
    ['added', 'Date added'],
    ['release', 'Release date'],
    ['year', 'Year'],
    ['rating', 'Rating'],
];
const ALPHABET = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'];
/**
 * Mixes collections into the titles. Sorted by title they slot in by name,
 * like Plex; any other sort puts them first. A collection that sorts after
 * every loaded title waits until the last page is in.
 */
function tiles(items, collections, sort, complete) {
    if (!collections.length)
        return items.map((item) => ({ item }));
    if (sort !== 'title')
        return [...collections.map((collection) => ({ collection })), ...items.map((item) => ({ item }))];
    const byName = (a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' });
    const cols = [...collections].sort((a, b) => byName(a.name, b.name));
    const out = [];
    let c = 0;
    for (const item of items) {
        while (c < cols.length && byName(cols[c].name, item.sortTitle || item.title) <= 0)
            out.push({ collection: cols[c++] });
        out.push({ item });
    }
    if (complete)
        while (c < cols.length)
            out.push({ collection: cols[c++] });
    return out;
}
// Keyed by id so switching libraries resets paging and restores that library's sort.
export default function LibraryPage() {
    const { id } = useParams();
    return _jsx(LibraryView, { id: id }, id);
}
function LibraryView({ id }) {
    const [params, setParams] = useSearchParams();
    // Recommended is the library's front page (Plex's default tab).
    const tab = params.get('tab') === 'collections' ? 'collections' : params.get('tab') === 'library' ? 'library' : 'recommended';
    const [sort, setSort] = useState(() => localStorage.getItem(`bb.sort.${id}`) ?? 'title');
    const [library, setLibrary] = useState(null);
    const [items, setItems] = useState([]);
    const [collections, setCollections] = useState([]);
    const [total, setTotal] = useState(0);
    const [letters, setLetters] = useState([]);
    const [loading, setLoading] = useState(false);
    const sentinel = useRef(null);
    const loadingRef = useRef(false);
    useDocumentTitle(library?.name);
    const generation = useRef(0);
    const load = useCallback(async (offset) => {
        // A new first page (another library or sort) always goes ahead, and a
        // page still on its way for the old one is dropped when it lands.
        if (offset === 0)
            generation.current++;
        else if (loadingRef.current)
            return;
        const gen = generation.current;
        loadingRef.current = true;
        setLoading(true);
        try {
            const page = await api.get(`/api/libraries/${id}/items?sort=${sort}&offset=${offset}&limit=${PAGE}`);
            if (gen !== generation.current)
                return;
            setLibrary(page.library);
            setTotal(page.total);
            setItems((prev) => {
                if (offset === 0)
                    return page.items;
                const have = new Set(prev.map((it) => it.id));
                return [...prev, ...page.items.filter((it) => !have.has(it.id))];
            });
            if (offset === 0) {
                setCollections(page.collections ?? []);
                setLetters(page.letters ?? []);
            }
        }
        finally {
            if (gen === generation.current) {
                loadingRef.current = false;
                setLoading(false);
            }
        }
    }, [id, sort]);
    useEffect(() => {
        setItems([]);
        load(0);
    }, [load]);
    useEffect(() => {
        const el = sentinel.current;
        if (!el)
            return;
        const obs = new IntersectionObserver((entries) => {
            if (entries[0].isIntersecting && items.length < total)
                load(items.length);
        }, { rootMargin: '800px' });
        obs.observe(el);
        return () => obs.disconnect();
    }, [items.length, total, load, tab]);
    // Jumping to a letter loads every page up to it first, then scrolls there.
    const jumpTo = async (l) => {
        let loaded = items;
        if (loaded.length <= l.offset && !loadingRef.current) {
            loadingRef.current = true;
            setLoading(true);
            try {
                while (loaded.length <= l.offset && loaded.length < total) {
                    const page = await api.get(`/api/libraries/${id}/items?sort=${sort}&offset=${loaded.length}&limit=500`);
                    if (!page.items.length)
                        break;
                    loaded = [...loaded, ...page.items];
                }
                setItems(loaded);
            }
            finally {
                loadingRef.current = false;
                setLoading(false);
            }
        }
        const target = loaded[l.offset];
        if (!target)
            return;
        requestAnimationFrame(() => document.getElementById(`lib-item-${target.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' }));
    };
    const changeSort = (s) => {
        localStorage.setItem(`bb.sort.${id}`, s);
        setSort(s);
    };
    // Plex-style A–Z bar on the Library tab when sorted by title.
    const showAZ = tab === 'library' && sort === 'title' && letters.length > 1;
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { children: [_jsx("h1", { children: library?.name ?? ' ' }), _jsxs("div", { className: "muted", children: [total, " ", library?.kind === 'shows' ? (total === 1 ? 'show' : 'shows') : total === 1 ? 'movie' : 'movies'] })] }), _jsxs("div", { className: "segmented", children: [_jsx("button", { className: tab === 'recommended' ? 'active' : '', onClick: () => setParams({}, { replace: true }), children: "Recommended" }), _jsx("button", { className: tab === 'library' ? 'active' : '', onClick: () => setParams({ tab: 'library' }, { replace: true }), children: "Library" }), _jsx("button", { className: tab === 'collections' ? 'active' : '', onClick: () => setParams({ tab: 'collections' }, { replace: true }), children: "Collections" })] }), tab === 'library' && (_jsxs("label", { className: "select-label", children: ["Sort", _jsx("select", { value: sort, onChange: (e) => changeSort(e.target.value), children: sorts.map(([v, label]) => (_jsx("option", { value: v, children: label }, v))) })] }))] }), tab === 'recommended' && library && _jsx(RecommendedTab, { library: library }), tab === 'collections' && _jsx(CollectionsTab, { libraryId: Number(id) }), tab === 'library' && (_jsxs(_Fragment, { children: [!loading && items.length === 0 && _jsx("div", { className: "empty", children: "This library is empty. Scans can take a few minutes for big folders." }), _jsx("div", { className: `poster-grid${showAZ ? ' has-az' : ''}`, children: tiles(items, collections, sort, items.length >= total).map((t) => 'item' in t ? (_jsx(PosterCard, { item: t.item, domId: showAZ ? `lib-item-${t.item.id}` : undefined }, t.item.id)) : (_jsx(CollectionCard, { collection: t.collection }, `c${t.collection.id}`))) }), showAZ && (_jsx("nav", { className: "az-bar", "aria-label": "Jump to letter", children: ALPHABET.map((ch) => {
                            const l = letters.find((x) => x.letter === ch);
                            return (_jsx("button", { type: "button", disabled: !l, onClick: () => l && jumpTo(l), "aria-label": ch === '#' ? 'Numbers and symbols' : ch, children: ch }, ch));
                        }) })), _jsx("div", { ref: sentinel, style: { height: 1 } })] }))] }));
}
function CollectionsTab({ libraryId }) {
    const { user } = useAuth();
    const { data, reload } = useApi(`/api/libraries/${libraryId}/collections`);
    const [creating, setCreating] = useState(false);
    return (_jsxs(_Fragment, { children: [user?.isAdmin && (_jsxs("div", { className: "actions", style: { marginBottom: 20 }, children: [_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setCreating(true), children: [_jsx(Icon, { name: "plus", size: 16 }), " New collection"] }), _jsx("span", { className: "muted small", children: "Add movies from their Edit dialog, or give a collection rules (genre, studio, years or a label) to fill it automatically." })] })), data?.length === 0 && _jsx("div", { className: "empty", children: "No collections yet. TMDB collections appear here once metadata is fetched." }), _jsx("div", { className: "poster-grid", children: data?.map((c) => (_jsx(CollectionCard, { collection: c, onChanged: reload }, c.id))) }), creating && (_jsx(CollectionEditor, { libraryId: libraryId, onClose: () => setCreating(false), onSaved: () => {
                    setCreating(false);
                    reload();
                } }))] }));
}
/** The library's front page: its rows, in the order an admin set. */
function RecommendedTab({ library }) {
    const { user } = useAuth();
    const { data, loading, reload } = useApi(`/api/libraries/${library.id}/hubs`);
    const [managing, setManaging] = useState(false);
    return (_jsxs(_Fragment, { children: [user?.isAdmin && (_jsxs("div", { className: "actions", style: { marginBottom: 20 }, children: [_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setManaging(true), children: [_jsx(Icon, { name: "sliders", size: 16 }), " Manage recommendations"] }), _jsx("span", { className: "muted small", children: "Pick which rows and collections show here, on Home and on Friends' Home." })] })), loading && _jsx("div", { className: "page-loading" }), data?.length === 0 && _jsx("div", { className: "empty", children: "Nothing to recommend yet. The Library tab has everything." }), _jsx("div", { className: "library-hubs", children: data?.map((row) => (_jsx(Row, { title: row.title, action: row.collectionId ? _jsx(Link, { to: `/collection/${row.collectionId}`, className: "muted small", children: "See all" }) : undefined, children: row.items.map((it) => (_jsx(PosterCard, { item: it }, it.id))) }, row.id))) }), managing && _jsx(RecommendationsEditor, { library: library, onClose: () => setManaging(false), onSaved: reload })] }));
}
