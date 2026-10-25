import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { api } from '../api';
import { Icon } from './Icon';
import { Modal } from './Modal';
import { moveItem } from './Pins';
const columns = [
    ['library', 'Library Recommended'],
    ['home', 'Home'],
    ['friends', "Friends' Home"],
];
/**
 * Plex's Manage Recommendations: every row a library can show (built-in rows
 * and its collections), dragged into order, each ticked for the library's
 * Recommended tab, Home, and Friends' Home.
 */
export function RecommendationsEditor({ library, onClose, onSaved }) {
    const [rows, setRows] = useState(null);
    const [dragging, setDragging] = useState(null);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    useEffect(() => {
        api.get(`/api/admin/libraries/${library.id}/rows`).then(setRows, (e) => setError(e.message));
    }, [library.id]);
    const toggle = (key, col) => setRows((cur) => cur && cur.map((r) => (r.key === key ? { ...r, [col]: !r[col] } : r)));
    const save = async () => {
        if (!rows)
            return;
        setSaving(true);
        try {
            await api.put(`/api/admin/libraries/${library.id}/rows`, rows);
            onSaved?.();
            onClose();
        }
        catch (e) {
            setError(e.message);
            setSaving(false);
        }
    };
    return (_jsxs(Modal, { title: `Manage recommendations · ${library.name}`, onClose: onClose, wide: true, children: [_jsx("p", { className: "muted small", style: { marginTop: 0 }, children: "Drag rows into order. Library Recommended is the library's front page. Home rows from every pinned library are merged, so all movie libraries share one Recently Added Movies row. Friends' Home is Home for people outside your household." }), !rows && !error && _jsx("div", { className: "page-loading" }), rows && (_jsxs("div", { className: "rec-table", role: "table", children: [_jsxs("div", { className: "rec-row rec-head", role: "row", children: [_jsx("span", {}), _jsx("span", { role: "columnheader", children: library.name }), columns.map(([c, label]) => (_jsx("span", { role: "columnheader", className: "rec-col", children: label }, c)))] }), rows.map((r, i) => (_jsxs("div", { role: "row", className: `rec-row ${dragging === i ? 'dragging' : ''}`, draggable: true, onDragStart: () => setDragging(i), onDragEnd: () => setDragging(null), onDragOver: (e) => {
                            e.preventDefault();
                            if (dragging !== null && dragging !== i) {
                                setRows((cur) => cur && moveItem(cur, dragging, i));
                                setDragging(i);
                            }
                        }, children: [_jsx("span", { className: "drag-handle", "aria-hidden": "true", children: _jsx(Icon, { name: "grip", size: 16 }) }), _jsxs("span", { className: "rec-title", children: [r.collectionId && _jsx(Icon, { name: "stack", size: 14 }), " ", r.title] }), columns.map(([c, label]) => (_jsx("span", { className: "rec-col", children: _jsx("input", { type: "checkbox", "aria-label": `${r.title}: ${label}`, checked: r[c], disabled: r.libraryOnly && c !== 'library', title: r.libraryOnly && c !== 'library' ? 'Home has its own Continue Watching' : undefined, onChange: () => toggle(r.key, c) }) }, c)))] }, r.key)))] })), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", style: { marginTop: 16, justifyContent: 'flex-end' }, children: [_jsx("button", { className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", disabled: !rows || saving, onClick: save, children: "Save" })] })] }));
}
