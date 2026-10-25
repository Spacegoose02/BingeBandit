import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../api';
import { useApi } from '../hooks';
import { Modal } from './Modal';
// Plex's per-collection Collection Mode, which overrides the library's.
const modes = [
    ['', 'Library default'],
    ['collapse', 'Hide items in this collection'],
    ['show', 'Show this collection and its items'],
    ['hide', 'Hide this collection but show its items'],
];
const yearValue = (s) => (s ? Number(s) : 0);
/** Create (libraryId) or edit (collection) a collection. */
export function CollectionEditor({ libraryId, collection, onClose, onSaved, }) {
    const [name, setName] = useState(collection?.name ?? '');
    const [overview, setOverview] = useState(collection?.overview ?? '');
    const [listUrl, setListUrl] = useState(collection?.listUrl ?? '');
    const [mode, setMode] = useState(collection?.mode ?? '');
    const [labelRule, setLabelRule] = useState(collection?.labelRule ?? '');
    const [genreRule, setGenreRule] = useState(collection?.genreRule ?? '');
    const [studioRule, setStudioRule] = useState(collection?.studioRule ?? '');
    const [yearFrom, setYearFrom] = useState(collection?.yearFrom ? String(collection.yearFrom) : '');
    const [yearTo, setYearTo] = useState(collection?.yearTo ? String(collection.yearTo) : '');
    const [error, setError] = useState('');
    const { data: facets } = useApi(`/api/admin/libraries/${libraryId}/facets`);
    const save = async (e) => {
        e.preventDefault();
        setError('');
        const rules = { listUrl, labelRule, genreRule, studioRule, yearFrom: yearValue(yearFrom), yearTo: yearValue(yearTo) };
        try {
            if (collection)
                await api.put(`/api/admin/collections/${collection.id}`, { name, overview, mode, ...rules });
            else
                await api.post(`/api/admin/libraries/${libraryId}/collections`, { name, ...rules });
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: collection ? `Edit ${collection.name}` : 'New collection', onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("label", { children: ["Name", _jsx("input", { value: name, onChange: (e) => setName(e.target.value), required: true, autoFocus: true, placeholder: "e.g. 80s Horror" })] }), collection && (_jsxs("label", { children: ["Summary", _jsx("textarea", { rows: 3, value: overview, onChange: (e) => setOverview(e.target.value) })] })), collection && (_jsxs("label", { children: ["Collection mode", _jsx("select", { value: mode, onChange: (e) => setMode(e.target.value), children: modes.map(([v, label]) => (_jsx("option", { value: v, children: label }, v))) }), _jsx("span", { className: "muted small", children: "How this collection and its titles show on the library's Library tab." })] })), _jsxs("label", { children: ["Follow a list", _jsx("input", { type: "url", value: listUrl, onChange: (e) => setListUrl(e.target.value), placeholder: "https://mdblist.com/lists/\u2026 or an RSS feed", spellCheck: false }), _jsx("span", { className: "muted small", children: "An MDBList list or an RSS feed (Letterboxd lists have one). Its titles in this library join the collection, and the \u201CSync list collections\u201D task keeps them up to date. Private MDBList lists need your key in Settings \u2192 Metadata." }), collection?.listUrl && collection.listUrl === listUrl && (_jsx("span", { className: collection.listError ? 'form-error' : 'muted small', children: collection.listError
                                ? `Last sync failed: ${collection.listError}`
                                : collection.listSyncedAt
                                    ? `Synced ${new Date(collection.listSyncedAt * 1000).toLocaleString()}`
                                    : 'Not synced yet' }))] }), _jsx("div", { className: "label", children: "Smart rules: include everything that matches all of these" }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Genre", _jsx("input", { list: "bb-genres", value: genreRule, onChange: (e) => setGenreRule(e.target.value), placeholder: "Any" })] }), _jsxs("label", { children: ["Studio or network", _jsx("input", { list: "bb-studios", value: studioRule, onChange: (e) => setStudioRule(e.target.value), placeholder: "Any" })] }), _jsxs("label", { children: ["From year", _jsx("input", { type: "number", min: 1870, max: 2200, value: yearFrom, onChange: (e) => setYearFrom(e.target.value), placeholder: "Any" })] }), _jsxs("label", { children: ["To year", _jsx("input", { type: "number", min: 1870, max: 2200, value: yearTo, onChange: (e) => setYearTo(e.target.value), placeholder: "Any" })] }), _jsxs("label", { className: "span-2", children: ["Label", _jsx("input", { list: "bb-labels", value: labelRule, onChange: (e) => setLabelRule(e.target.value), placeholder: "Any" })] })] }), _jsx("datalist", { id: "bb-genres", children: facets?.genres.map((g) => _jsx("option", { value: g }, g)) }), _jsx("datalist", { id: "bb-studios", children: facets?.studios.map((g) => _jsx("option", { value: g }, g)) }), _jsx("datalist", { id: "bb-labels", children: facets?.labels.map((l) => _jsx("option", { value: l }, l)) }), _jsx("p", { className: "muted small", children: "Leave every rule empty for a hand-picked collection. Labels come from a movie's or show's Edit \u2192 Tags tab. Anything that matches joins the collection automatically, alongside anything added by hand." }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: collection ? 'Save' : 'Create' })] })] }) }));
}
