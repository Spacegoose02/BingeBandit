import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { api } from '../../api';
import { LogoField } from '../../components/ChannelLogos';
import { Icon } from '../../components/Icon';
import { Modal } from '../../components/Modal';
import { useApi } from '../../hooks';
import { clock } from '../LiveGuide';
const toTime = (m) => `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
const fromTime = (s) => {
    const [h, m] = s.split(':').map(Number);
    return h * 60 + m;
};
const sourceKey = (s) => `${s.type}:${s.id ?? s.label}`;
/** Create or edit a custom (library-built) channel and its time blocks. */
export function CustomChannelEditor({ channel, nextNumber, onClose, onSaved }) {
    const [form, setForm] = useState({ number: channel?.number ?? nextNumber, name: channel?.name ?? '', logo: channel?.logo ?? '', group: channel?.group ?? 'Custom', enabled: channel?.enabled ?? true });
    const [blocks, setBlocks] = useState([{ startMinute: -1, endMinute: -1, mode: 'shuffle', sources: [] }]);
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);
    const [savedId, setSavedId] = useState(channel?.id ?? null);
    const { data: existing } = useApi(channel ? `/api/admin/live/custom/${channel.id}` : null);
    const { data: schedule, reload: reloadSchedule } = useApi(savedId ? `/api/admin/live/custom/${savedId}/schedule` : null);
    const { data: options } = useApi('/api/admin/live/content-sources');
    useEffect(() => {
        if (existing?.blocks.length)
            setBlocks(existing.blocks);
    }, [existing]);
    // Sources are stored without names; look them up for display.
    const nameOf = (s) => s.name ?? options?.find((o) => sourceKey(o) === sourceKey(s))?.name ?? s.label ?? `${s.type} ${s.id}`;
    const update = (i, patch) => setBlocks(blocks.map((b, j) => (j === i ? { ...b, ...patch } : b)));
    const save = async (e) => {
        e.preventDefault();
        setSaving(true);
        setError('');
        try {
            const body = { ...form, blocks: blocks.map((b) => ({ ...b, sources: b.sources.map((s) => ({ type: s.type, id: s.id, label: s.label, name: nameOf(s) })) })) };
            const res = channel || savedId
                ? await api.put(`/api/admin/live/custom/${savedId}`, body)
                : await api.post('/api/admin/live/custom', body);
            setSavedId(res.channel.id);
            reloadSchedule();
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setSaving(false);
        }
    };
    return (_jsx(Modal, { title: channel ? `Edit ${channel.name}` : 'New custom channel', onClose: onClose, wide: true, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsx("p", { className: "muted small", style: { margin: 0 }, children: "A 24/7 channel made from your own library. It shows up in the guide and plays like any live channel." }), _jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Number", _jsx("input", { type: "number", min: 1, step: "0.1", value: form.number, onChange: (e) => setForm({ ...form, number: Number(e.target.value) }), required: true })] }), _jsxs("label", { children: ["Name", _jsx("input", { value: form.name, onChange: (e) => setForm({ ...form, name: e.target.value }), placeholder: "Cartoon Classics", required: true })] }), _jsxs("label", { children: ["Category", _jsx("input", { value: form.group, onChange: (e) => setForm({ ...form, group: e.target.value }) })] })] }), _jsx("div", { className: "label", children: "Logo (optional)" }), _jsx(LogoField, { value: form.logo, onChange: (logo) => setForm({ ...form, logo }) }), _jsx("div", { className: "label", children: "What plays" }), blocks.map((b, i) => (_jsxs("div", { className: "panel block-editor", children: [_jsxs("div", { className: "block-head", children: [_jsxs("select", { value: b.startMinute < 0 ? 'all' : 'timed', onChange: (e) => update(i, e.target.value === 'all' ? { startMinute: -1, endMinute: -1 } : { startMinute: 18 * 60, endMinute: 21 * 60 }), children: [_jsx("option", { value: "all", children: "All day (default)" }), _jsx("option", { value: "timed", children: "Between\u2026" })] }), b.startMinute >= 0 && (_jsxs(_Fragment, { children: [_jsx("input", { type: "time", value: toTime(b.startMinute), onChange: (e) => update(i, { startMinute: fromTime(e.target.value) }) }), _jsx("span", { className: "muted", children: "and" }), _jsx("input", { type: "time", value: toTime(b.endMinute % 1440), onChange: (e) => update(i, { endMinute: fromTime(e.target.value) || 1440 }) })] })), _jsxs("select", { value: b.mode, onChange: (e) => update(i, { mode: e.target.value }), children: [_jsx("option", { value: "shuffle", children: "Shuffle" }), _jsx("option", { value: "order", children: "In order" })] }), _jsx("div", { className: "spacer" }), blocks.length > 1 && (_jsx("button", { type: "button", className: "icon-btn danger", onClick: () => setBlocks(blocks.filter((_, j) => j !== i)), "aria-label": "Remove block", children: _jsx(Icon, { name: "trash", size: 16 }) }))] }), _jsxs("div", { className: "chips", children: [b.sources.map((s) => (_jsxs("span", { className: "chip removable", children: [nameOf(s), " ", _jsx("span", { className: "muted small", children: s.type }), _jsx("button", { type: "button", onClick: () => update(i, { sources: b.sources.filter((x) => sourceKey(x) !== sourceKey(s)) }), "aria-label": "Remove source", children: _jsx(Icon, { name: "x", size: 12 }) })] }, sourceKey(s)))), b.sources.length === 0 && _jsx("span", { className: "muted small", children: "No content yet" })] }), _jsx(SourcePicker, { onPick: (o) => !b.sources.some((s) => sourceKey(s) === sourceKey(o)) && update(i, { sources: [...b.sources, o] }) })] }, i))), _jsxs("button", { type: "button", className: "btn btn-glass btn-sm", style: { alignSelf: 'flex-start' }, onClick: () => setBlocks([...blocks, { startMinute: 6 * 60, endMinute: 9 * 60, mode: 'shuffle', sources: [] }]), children: [_jsx(Icon, { name: "plus", size: 16 }), " Add a time block"] }), _jsx("p", { className: "muted small", style: { margin: 0 }, children: "Time blocks override the all-day block, e.g. cartoons from 6\u20139 AM. Shuffle plays everything once before repeating; In order continues where it left off." }), schedule && schedule.length > 0 && (_jsxs(_Fragment, { children: [_jsx("div", { className: "label", children: "Coming up" }), _jsx("div", { className: "schedule-preview", children: schedule.slice(0, 12).map((p) => (_jsxs("div", { className: "schedule-row", children: [_jsx("span", { className: "muted mono", children: clock(p.start) }), _jsx("span", { children: p.title }), p.subtitle && _jsx("span", { className: "muted small", children: p.subtitle })] }, p.id))) })] })), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsxs("label", { className: "check grow", children: [_jsx("input", { type: "checkbox", checked: form.enabled, onChange: (e) => setForm({ ...form, enabled: e.target.checked }) }), " Visible to viewers"] }), _jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Close" }), _jsx("button", { className: "btn btn-primary", disabled: saving, children: saving ? 'Building schedule…' : savedId ? 'Save' : 'Create channel' })] })] }) }));
}
function SourcePicker({ onPick }) {
    const [q, setQ] = useState('');
    const { data } = useApi(q.trim() ? `/api/admin/live/content-sources?q=${encodeURIComponent(q.trim())}` : null);
    const needle = q.trim().toLowerCase();
    const matches = data?.filter((o) => o.type === 'show' || o.name.toLowerCase().includes(needle)).slice(0, 12);
    return (_jsxs("div", { className: "source-picker", children: [_jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Add content: search libraries, shows, collections, playlists, labels" }), q.trim() && (_jsxs("div", { className: "pick-list", children: [matches?.map((o) => (_jsxs("button", { type: "button", onClick: () => {
                            onPick({ type: o.type, id: o.id, label: o.label, name: o.name });
                            setQ('');
                        }, children: [o.name, " ", _jsx("span", { className: "muted small", children: o.detail })] }, `${o.type}:${o.id ?? o.label}`))), matches?.length === 0 && _jsx("div", { className: "muted small menu-pad", children: "Nothing matches" })] }))] }));
}
