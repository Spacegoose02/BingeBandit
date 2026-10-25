import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useRef, useState } from 'react';
import { api } from '../../api';
import { libraryRef, LOGO_TYPES, uploadLogos } from '../../components/ChannelLogos';
import { Icon } from '../../components/Icon';
import { Modal } from '../../components/Modal';
import { useApi } from '../../hooks';
import { ChannelLogo as Logo, channelNo } from '../LiveGuide';
/** Admin → Live TV → Logos: your own channel logos, which survive playlist refreshes. */
export function Logos() {
    const { data, reload } = useApi('/api/admin/live/logos');
    const [match, setMatch] = useState(() => localStorage.getItem('bb.logoMatch') !== '0');
    const [note, setNote] = useState('');
    const [busy, setBusy] = useState(false);
    const [q, setQ] = useState('');
    const [assigning, setAssigning] = useState(null);
    const fileRef = useRef(null);
    const upload = async (files) => {
        setBusy(true);
        setNote('');
        try {
            const res = await uploadLogos(files, match);
            const parts = [`Added ${res.added} logo${res.added === 1 ? '' : 's'}.`];
            if (match)
                parts.push(`${res.matched} channel${res.matched === 1 ? '' : 's'} now use${res.matched === 1 ? 's' : ''} them.`);
            if (res.failed?.length)
                parts.push(`Skipped: ${res.failed.join('; ')}`);
            setNote(parts.join(' '));
            reload();
        }
        catch (e) {
            setNote(e.message);
        }
        finally {
            setBusy(false);
            if (fileRef.current)
                fileRef.current.value = '';
        }
    };
    const matchAll = async () => {
        const res = await api.post('/api/admin/live/logos/match');
        setNote(res.matched ? `${res.matched} channel${res.matched === 1 ? '' : 's'} got a logo from the library.` : 'Every channel with a matching name already uses its logo.');
        reload();
    };
    const rename = async (l) => {
        const name = prompt('Logo name (used to match channel names)', l.name)?.trim();
        if (!name || name === l.name)
            return;
        await api.put(`/api/admin/live/logos/${l.id}`, { name });
        reload();
    };
    const remove = async (l) => {
        const used = l.channels ? ` ${l.channels} channel${l.channels === 1 ? '' : 's'} will go back to the provider's logo.` : '';
        if (!confirm(`Delete the logo “${l.name}”?${used}`))
            return;
        await api.del(`/api/admin/live/logos/${l.id}`);
        reload();
    };
    const shown = (data ?? []).filter((l) => !q || l.name.toLowerCase().includes(q.toLowerCase()));
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "actions", style: { marginBottom: 8 }, children: [_jsxs("button", { className: "btn btn-primary", disabled: busy, onClick: () => fileRef.current?.click(), children: [_jsx(Icon, { name: "upload", size: 18 }), " ", busy ? 'Uploading…' : 'Upload logos'] }), _jsx("input", { ref: fileRef, type: "file", accept: LOGO_TYPES, multiple: true, hidden: true, onChange: (e) => e.target.files?.length && upload(e.target.files) }), _jsxs("label", { className: "check toolbar-check", children: [_jsx("input", { type: "checkbox", checked: match, onChange: (e) => {
                                    setMatch(e.target.checked);
                                    try {
                                        localStorage.setItem('bb.logoMatch', e.target.checked ? '1' : '0');
                                    }
                                    catch {
                                        /* private mode */
                                    }
                                } }), "Give each to channels with the same name"] }), _jsxs("button", { className: "btn btn-glass", onClick: matchAll, disabled: !data?.length, children: [_jsx(Icon, { name: "refresh", size: 18 }), " Match all to channels"] }), _jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Find a logo", style: { width: 200 } }), _jsxs("span", { className: "muted small", children: [data?.length ?? 0, " logos"] })] }), _jsx("p", { className: "muted small", style: { marginTop: 0 }, children: "PNG, JPEG, SVG, WebP or GIF up to 2 MB each. Name files after the channel (\u201CCNN.png\u201D, \u201CTSN 1.svg\u201D): matching ignores case, \u201CHD\u201D, country prefixes like \u201CUS:\u201D and text in brackets. Library logos stay when playlists refresh." }), note && _jsx("div", { className: "notice small", style: { marginBottom: 12 }, children: note }), _jsx("div", { className: "logo-library", children: shown.map((l) => (_jsxs("div", { className: "logo-card panel", children: [_jsx("div", { className: "art-thumb logo", children: _jsx("img", { src: l.url, alt: "", loading: "lazy" }) }), _jsx("button", { className: "logo-name", onClick: () => rename(l), title: "Rename", children: l.name }), _jsx("div", { className: "muted small", children: l.channels ? `${l.channels} channel${l.channels === 1 ? '' : 's'}` : 'Not used' }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setAssigning(l), children: [_jsx(Icon, { name: "tv", size: 16 }), " Channels"] }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(l), "aria-label": "Delete logo", children: _jsx(Icon, { name: "trash", size: 16 }) })] })] }, l.id))) }), data?.length === 0 && _jsx("div", { className: "panel list", children: _jsx("div", { className: "list-row muted", children: "No logos yet. Upload a few, or a whole folder's worth at once." }) }), assigning && _jsx(AssignLogo, { logo: assigning, onClose: () => setAssigning(null), onSaved: () => { setAssigning(null); reload(); } })] }));
}
/** Pick which channels use a logo. */
function AssignLogo({ logo, onClose, onSaved }) {
    const { data } = useApi('/api/admin/live/channels');
    const [picked, setPicked] = useState(null);
    const [q, setQ] = useState('');
    const [error, setError] = useState('');
    const using = new Set((data ?? []).filter((c) => c.logo === libraryRef(logo)).map((c) => c.id));
    const sel = picked ?? using;
    const shown = (data ?? []).filter((c) => !q || c.name.toLowerCase().includes(q.toLowerCase()) || String(c.number).startsWith(q));
    const toggle = (id) => {
        const n = new Set(sel);
        if (n.has(id))
            n.delete(id);
        else
            n.add(id);
        setPicked(n);
    };
    const save = async () => {
        try {
            await api.post(`/api/admin/live/logos/${logo.id}/assign`, { channelIds: [...sel] });
            onSaved();
        }
        catch (e) {
            setError(e.message);
        }
    };
    return (_jsx(Modal, { title: `Channels using “${logo.name}”`, onClose: onClose, children: _jsxs("div", { className: "form", children: [_jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Find a channel", autoFocus: true }), _jsx("div", { className: "panel list compact assign-list", children: shown.map((c) => (_jsxs("label", { className: "list-row", children: [_jsx("input", { type: "checkbox", checked: sel.has(c.id), onChange: () => toggle(c.id) }), _jsx("span", { className: "ch-num", children: channelNo(c.number) }), _jsx(Logo, { channel: { name: c.name, logo: c.logoUrl }, size: 22 }), _jsx("span", { className: "grow", children: c.name })] }, c.id))) }), _jsx("p", { className: "muted small", style: { margin: 0 }, children: "Channels you untick go back to their provider's logo." }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsxs("span", { className: "muted small grow", children: [sel.size, " selected"] }), _jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", onClick: save, children: "Save" })] })] }) }));
}
