import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../api';
import { Icon } from './Icon';
import { Modal } from './Modal';
const channelNo = (n) => (Number.isInteger(n) ? String(n) : n.toFixed(1));
/** Create, rename, order and fill your own channel categories. */
export function ManageCategories({ categories, channels, onClose, onChanged, }) {
    const [editing, setEditing] = useState(null);
    const [error, setError] = useState('');
    const favorites = channels.filter((c) => c.favorite).map((c) => c.id);
    const run = async (fn) => {
        setError('');
        try {
            await fn();
            onChanged();
        }
        catch (e) {
            setError(e.message);
        }
    };
    const create = (fromFavorites) => {
        const name = prompt(fromFavorites ? 'Name for a category with your favourite channels' : 'Name for the new category')?.trim();
        if (!name)
            return;
        run(() => api.post('/api/live/categories', { name, channelIds: fromFavorites ? favorites : [] }));
    };
    const rename = (c) => {
        const name = prompt('Category name', c.name)?.trim();
        if (name && name !== c.name)
            run(() => api.put(`/api/live/categories/${c.id}`, { ...c, name }));
    };
    const remove = (c) => confirm(`Delete the category “${c.name}”? The channels stay.`) && run(() => api.del(`/api/live/categories/${c.id}`));
    const move = (i, dir) => {
        const ids = categories.map((c) => c.id);
        const j = i + dir;
        [ids[i], ids[j]] = [ids[j], ids[i]];
        run(() => api.put('/api/live/categories/order', { ids }));
    };
    if (editing) {
        return (_jsx(PickChannels, { category: editing, channels: channels, onClose: () => setEditing(null), onSaved: () => {
                setEditing(null);
                onChanged();
            } }));
    }
    return (_jsx(Modal, { title: "My categories", onClose: onClose, children: _jsxs("div", { className: "form", children: [_jsx("p", { className: "muted small", style: { margin: 0 }, children: "Your own channel groups, shown in the guide's filter. Only you see them." }), _jsxs("div", { className: "panel list compact", children: [categories.map((c, i) => (_jsxs("div", { className: "list-row", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { children: c.name }), _jsxs("div", { className: "muted small", children: [c.channelIds.length, " channel", c.channelIds.length === 1 ? '' : 's'] })] }), _jsx("button", { type: "button", className: "icon-btn", onClick: () => move(i, -1), disabled: i === 0, "aria-label": "Move up", children: _jsx(Icon, { name: "arrowUp", size: 16 }) }), _jsx("button", { type: "button", className: "icon-btn", onClick: () => move(i, 1), disabled: i === categories.length - 1, "aria-label": "Move down", children: _jsx(Icon, { name: "arrowDown", size: 16 }) }), _jsxs("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setEditing(c), children: [_jsx(Icon, { name: "tv", size: 16 }), " Channels"] }), _jsx("button", { type: "button", className: "icon-btn", onClick: () => rename(c), "aria-label": "Rename", children: _jsx(Icon, { name: "edit", size: 16 }) }), _jsx("button", { type: "button", className: "icon-btn danger", onClick: () => remove(c), "aria-label": "Delete category", children: _jsx(Icon, { name: "trash", size: 16 }) })] }, c.id))), categories.length === 0 && _jsx("div", { className: "list-row muted", children: "No categories yet." })] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", children: [_jsxs("button", { type: "button", className: "btn btn-primary", onClick: () => create(false), children: [_jsx(Icon, { name: "plus", size: 18 }), " New category"] }), _jsxs("button", { type: "button", className: "btn btn-glass", onClick: () => create(true), disabled: favorites.length === 0, title: favorites.length ? '' : 'Star some channels first', children: [_jsx(Icon, { name: "star", size: 18 }), " From my favourites (", favorites.length, ")"] })] })] }) }));
}
function PickChannels({ category, channels, onClose, onSaved }) {
    const [sel, setSel] = useState(() => new Set(category.channelIds));
    const [q, setQ] = useState('');
    const [error, setError] = useState('');
    // Favourites first: they're the usual starting point for a category.
    const sorted = [...channels].sort((a, b) => Number(b.favorite) - Number(a.favorite));
    const shown = sorted.filter((c) => !q || c.name.toLowerCase().includes(q.toLowerCase()) || String(c.number).startsWith(q) || (c.group ?? '').toLowerCase().includes(q.toLowerCase()));
    const toggle = (id) => {
        const n = new Set(sel);
        if (n.has(id))
            n.delete(id);
        else
            n.add(id);
        setSel(n);
    };
    const save = async () => {
        try {
            await api.put(`/api/live/categories/${category.id}`, { name: category.name, channelIds: [...sel] });
            onSaved();
        }
        catch (e) {
            setError(e.message);
        }
    };
    return (_jsx(Modal, { title: `Channels in ${category.name}`, onClose: onClose, children: _jsxs("div", { className: "form", children: [_jsxs("div", { className: "actions", children: [_jsx("input", { className: "grow", value: q, onChange: (e) => setQ(e.target.value), placeholder: "Find a channel or provider category", autoFocus: true }), _jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setSel(new Set([...sel, ...shown.map((c) => c.id)])), children: "Tick all shown" })] }), _jsx("div", { className: "panel list compact assign-list", children: shown.map((c) => (_jsxs("label", { className: "list-row", children: [_jsx("input", { type: "checkbox", checked: sel.has(c.id), onChange: () => toggle(c.id) }), _jsx("span", { className: "ch-num", children: channelNo(c.number) }), _jsx("span", { className: "grow", children: c.name }), c.favorite && _jsx(Icon, { name: "star", fill: true, size: 14, className: "fav-mark" }), c.group && _jsx("span", { className: "muted small", children: c.group })] }, c.id))) }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsxs("span", { className: "muted small grow", children: [sel.size, " selected"] }), _jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Back" }), _jsx("button", { type: "button", className: "btn btn-primary", onClick: save, children: "Save" })] })] }) }));
}
/** "Add to category" for one channel: tick or untick it in each of your categories, or start a new one. */
export function CategoryMenu({ channel, categories, onChanged }) {
    const pick = async (v) => {
        if (v === 'new') {
            const name = prompt(`New category with ${channel.name}`)?.trim();
            if (name)
                await api.post('/api/live/categories', { name, channelIds: [channel.id] });
        }
        else if (v) {
            const cat = categories.find((c) => String(c.id) === v);
            if (!cat)
                return;
            const inIt = cat.channelIds.includes(channel.id);
            await api[inIt ? 'del' : 'post'](`/api/live/categories/${cat.id}/channels/${channel.id}`);
        }
        onChanged();
    };
    return (_jsxs("select", { className: "btn btn-glass category-menu", value: "", onChange: (e) => pick(e.target.value), "aria-label": `Add ${channel.name} to a category`, children: [_jsx("option", { value: "", children: "Add to category\u2026" }), categories.map((c) => (_jsxs("option", { value: c.id, children: [c.channelIds.includes(channel.id) ? '✓ ' : ' ', c.name] }, c.id))), _jsx("option", { value: "new", children: "+ New category\u2026" })] }));
}
