import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../api';
import { Icon } from './Icon';
import { Modal } from './Modal';
/** Moves the item at from to to, for drag and drop reordering. */
export function moveItem(list, from, to) {
    const next = list.slice();
    const [it] = next.splice(from, 1);
    next.splice(to, 0, it);
    return next;
}
/** Fired on window after pins change, so Home can rebuild its rows. */
export const pinsChanged = 'bb:pins-changed';
export async function savePins(libraries) {
    await api.put('/api/me/pins', { libraries: libraries.map((l) => ({ id: l.id, pinned: l.pinned })) });
    window.dispatchEvent(new Event(pinsChanged));
}
/**
 * Pick which libraries sit in the sidebar and feed Home, and drag them into
 * order. Shown once after the first sign-in, and from the sidebar's Edit.
 */
export function PinsDialog({ libraries, first, onClose, onSaved }) {
    const [list, setList] = useState(() => [...libraries.filter((l) => l.pinned), ...libraries.filter((l) => !l.pinned)]);
    const [dragging, setDragging] = useState(null);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    const save = async () => {
        setSaving(true);
        try {
            await savePins(list);
            onSaved();
        }
        catch (e) {
            setError(e.message);
            setSaving(false);
        }
    };
    // Skipping the first-time question keeps everything pinned and stops asking.
    const close = first ? () => savePins(libraries).finally(onClose) : onClose;
    return (_jsxs(Modal, { title: first ? 'What would you like to pin?' : 'Pinned libraries', onClose: close, children: [_jsx("p", { className: "muted small", style: { marginTop: 0 }, children: "Pinned libraries sit in the sidebar and fill your Home. Drag to change the order. Unpinned ones stay under More." }), _jsx("div", { className: "pin-list", children: list.map((l, i) => (_jsxs("div", { className: `pin-row ${dragging === i ? 'dragging' : ''}`, draggable: true, onDragStart: () => setDragging(i), onDragEnd: () => setDragging(null), onDragOver: (e) => {
                        e.preventDefault();
                        if (dragging !== null && dragging !== i) {
                            setList((cur) => moveItem(cur, dragging, i));
                            setDragging(i);
                        }
                    }, children: [_jsx("span", { className: "drag-handle", "aria-hidden": "true", children: _jsx(Icon, { name: "grip", size: 16 }) }), _jsx(Icon, { name: l.kind === 'movies' ? 'film' : 'tv', size: 18 }), _jsx("span", { className: "pin-name", children: l.name }), _jsx("button", { className: `pin-toggle ${l.pinned ? 'on' : ''}`, "aria-pressed": l.pinned, title: l.pinned ? 'Unpin' : 'Pin', onClick: () => setList((cur) => cur.map((x) => (x.id === l.id ? { ...x, pinned: !x.pinned } : x))), children: _jsx(Icon, { name: "pin", size: 16 }) })] }, l.id))) }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", style: { marginTop: 16, justifyContent: 'flex-end' }, children: [!first && (_jsx("button", { className: "btn btn-glass", onClick: onClose, children: "Cancel" })), _jsx("button", { className: "btn btn-primary", disabled: saving, onClick: save, children: first ? 'Done' : 'Save' })] })] }));
}
