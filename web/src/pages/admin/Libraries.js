import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../../api';
import { Icon } from '../../components/Icon';
import { Modal } from '../../components/Modal';
import { useApi, useDocumentTitle } from '../../hooks';
import { timeAgo } from '../../format';
import { ScanBadge } from './Dashboard';
export default function AdminLibraries() {
    useDocumentTitle('Libraries');
    const { data: libraries, reload } = useApi('/api/admin/libraries');
    const { data: scans } = useApi('/api/admin/scan', 2000);
    const [editing, setEditing] = useState(null);
    const act = async (fn) => {
        try {
            await fn();
            reload();
        }
        catch (e) {
            alert(e.message);
        }
    };
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsx("h1", { children: "Libraries" }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-glass", onClick: () => act(() => api.post('/api/admin/scan')), children: [_jsx(Icon, { name: "refresh", size: 18 }), " Scan all"] }), _jsxs("button", { className: "btn btn-primary", onClick: () => setEditing('new'), children: [_jsx(Icon, { name: "plus", size: 18 }), " Add library"] })] })] }), _jsx("div", { className: "lib-grid", children: libraries?.map((l) => {
                    const scan = scans?.find((s) => s.libraryId === l.id);
                    return (_jsxs("div", { className: "panel lib-card", children: [_jsxs("div", { className: "lib-card-head", children: [_jsx(Icon, { name: l.kind === 'movies' ? 'film' : 'tv', size: 24 }), _jsxs("div", { className: "grow", children: [_jsx("h3", { children: l.name }), _jsxs("div", { className: "muted small", children: [l.kind === 'movies' ? 'Movies' : 'TV Shows', " \u00B7 ", l.itemCount, " items \u00B7 scanned ", timeAgo(l.lastScan)] })] })] }), _jsx("ul", { className: "path-list", children: l.paths?.map((p) => (_jsxs("li", { className: "mono small", children: [_jsx(Icon, { name: "folder", size: 14 }), " ", p] }, p))) }), scan?.running && _jsx(ScanBadge, { scan: scan }), scan && !scan.running && scan.error && _jsxs("div", { className: "form-error small", children: ["Last scan failed: ", scan.error] }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-glass btn-sm", disabled: scan?.running, onClick: () => act(() => api.post(`/api/admin/libraries/${l.id}/scan`)), children: [_jsx(Icon, { name: "refresh", size: 16 }), " Scan"] }), _jsxs("button", { className: "btn btn-glass btn-sm", disabled: scan?.running, onClick: () => act(() => api.post(`/api/admin/libraries/${l.id}/refresh`)), children: [_jsx(Icon, { name: "download", size: 16 }), " Refresh metadata"] }), _jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing(l), children: [_jsx(Icon, { name: "edit", size: 16 }), " Edit"] }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => confirm(`Remove "${l.name}"? Your files stay on disk; watch history for this library is lost.`) && act(() => api.del(`/api/admin/libraries/${l.id}`)), children: _jsx(Icon, { name: "trash", size: 16 }) })] })] }, l.id));
                }) }), libraries?.length === 0 && _jsx("div", { className: "empty", children: "No libraries yet. Add a folder of movies or TV shows to get started." }), editing && (_jsx(LibraryEditor, { library: editing === 'new' ? null : editing, onClose: () => setEditing(null), onSaved: () => {
                    setEditing(null);
                    reload();
                } }))] }));
}
function LibraryEditor({ library, onClose, onSaved }) {
    const [name, setName] = useState(library?.name ?? '');
    const [kind, setKind] = useState(library?.kind ?? 'movies');
    const [paths, setPaths] = useState(library?.paths ?? []);
    const [picking, setPicking] = useState(paths.length === 0);
    const [detect, setDetect] = useState(!library?.skipMarkers);
    const [collectionMode, setCollectionMode] = useState(library?.collectionMode ?? 'show');
    const [error, setError] = useState('');
    const save = async (e) => {
        e.preventDefault();
        setError('');
        try {
            const body = { name, kind, paths, skipMarkers: !detect, collectionMode };
            if (library)
                await api.put(`/api/admin/libraries/${library.id}`, body);
            else
                await api.post('/api/admin/libraries', body);
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: library ? `Edit ${library.name}` : 'Add library', onClose: onClose, wide: true, children: _jsxs("form", { className: "form", onSubmit: save, children: [!library && (_jsx("div", { className: "kind-picker", children: ['movies', 'shows'].map((k) => (_jsxs("button", { type: "button", className: kind === k ? 'active' : '', onClick: () => setKind(k), children: [_jsx(Icon, { name: k === 'movies' ? 'film' : 'tv', size: 26 }), k === 'movies' ? 'Movies' : 'TV Shows'] }, k))) })), _jsxs("label", { children: ["Name", _jsx("input", { value: name, onChange: (e) => setName(e.target.value), placeholder: kind === 'movies' ? 'Movies' : 'TV Shows', required: true })] }), _jsxs("div", { children: [_jsx("div", { className: "label", children: "Folders" }), paths.map((p) => (_jsxs("div", { className: "path-row", children: [_jsx(Icon, { name: "folder", size: 16 }), _jsx("span", { className: "mono grow", children: p }), _jsx("button", { type: "button", className: "icon-btn", onClick: () => setPaths(paths.filter((x) => x !== p)), "aria-label": "Remove folder", children: _jsx(Icon, { name: "x", size: 16 }) })] }, p))), picking ? (_jsx(FolderPicker, { onPick: (p) => {
                                if (!paths.includes(p))
                                    setPaths([...paths, p]);
                                setPicking(false);
                            }, onCancel: paths.length ? () => setPicking(false) : undefined })) : (_jsxs("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setPicking(true), children: [_jsx(Icon, { name: "plus", size: 16 }), " Add folder"] }))] }), _jsx("p", { className: "muted small", children: kind === 'movies'
                        ? 'Tip: name folders "Movie Title (Year)" for the best matches.'
                        : 'Tip: use "Show Name/Season 01/Show Name - S01E01.mkv" for the best matches.' }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: detect, onChange: (e) => setDetect(e.target.checked) }), kind === 'movies' ? 'Detect end credits (for the Skip Credits button)' : 'Detect intros and end credits (for the Skip buttons)'] }), _jsxs("label", { children: ["Collections on the Library tab", _jsxs("select", { value: collectionMode, onChange: (e) => setCollectionMode(e.target.value), children: [_jsx("option", { value: "show", children: "Show collections and their items" }), _jsx("option", { value: "hide", children: "Hide collections but show their items" }), _jsx("option", { value: "collapse", children: "Hide items which are in collections" })] })] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", disabled: !name || paths.length === 0, children: library ? 'Save' : 'Add library' })] })] }) }));
}
function FolderPicker({ onPick, onCancel }) {
    const [path, setPath] = useState('');
    const [typed, setTyped] = useState('');
    const { data, error } = useApi(`/api/admin/browse?path=${encodeURIComponent(path)}`);
    return (_jsxs("div", { className: "folder-picker", children: [_jsxs("div", { className: "folder-bar", children: [_jsx("button", { type: "button", className: "icon-btn", disabled: !data?.path, onClick: () => setPath(data?.parent ?? ''), "aria-label": "Up", children: _jsx(Icon, { name: "back", size: 18 }) }), _jsx("input", { className: "mono", value: typed || data?.path || '', placeholder: "Type a path or browse below", onChange: (e) => setTyped(e.target.value), onKeyDown: (e) => {
                            if (e.key === 'Enter') {
                                e.preventDefault();
                                setPath(typed);
                                setTyped('');
                            }
                        } })] }), error && _jsx("div", { className: "form-error small", children: error }), _jsxs("div", { className: "folder-list", children: [data?.dirs.map((d) => (_jsxs("button", { type: "button", onClick: () => { setPath(d.path); setTyped(''); }, children: [_jsx(Icon, { name: "folder", size: 16 }), " ", d.name] }, d.path))), data && data.dirs.length === 0 && _jsx("div", { className: "muted small pad", children: "No sub-folders" })] }), _jsxs("div", { className: "actions end", children: [onCancel && _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: onCancel, children: "Cancel" }), _jsx("button", { type: "button", className: "btn btn-primary btn-sm", disabled: !data?.path, onClick: () => data && onPick(data.path), children: "Use this folder" })] })] }));
}
