import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { api } from '../../api';
import { Modal } from '../../components/Modal';
// DispatcharrImport copies provider accounts, guides and channels from a
// Dispatcharr server. The login is only used for the import.
export function DispatcharrImport({ onClose, onChanged }) {
    const [form, setForm] = useState({ url: '', username: '', password: '', apiKey: '' });
    const [useKey, setUseKey] = useState(false);
    const [plan, setPlan] = useState(null);
    const [status, setStatus] = useState(null);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const body = () => (useKey ? { url: form.url, apiKey: form.apiKey } : { url: form.url, username: form.username, password: form.password });
    // Follow a running import (also one started before the dialog opened).
    useEffect(() => {
        let timer = 0;
        let stopped = false;
        const poll = async () => {
            try {
                const s = await api.get('/api/admin/live/dispatcharr/import');
                if (stopped)
                    return;
                if (s.startedAt)
                    setStatus(s);
                if (s.running)
                    timer = window.setTimeout(poll, 1000);
                else if (status?.running)
                    onChanged();
            }
            catch {
                /* ignore */
            }
        };
        poll();
        return () => {
            stopped = true;
            clearTimeout(timer);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [status?.running]);
    const connect = async (e) => {
        e.preventDefault();
        setBusy(true);
        setError('');
        setPlan(null);
        try {
            setPlan(await api.post('/api/admin/live/dispatcharr/preview', body()));
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    const start = async () => {
        setBusy(true);
        setError('');
        try {
            setStatus(await api.post('/api/admin/live/dispatcharr/import', body()));
            setPlan(null);
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });
    const running = !!status?.running;
    return (_jsx(Modal, { title: "Import from Dispatcharr", onClose: onClose, wide: true, children: _jsxs("div", { className: "form", children: [status && (_jsxs("div", { className: "panel", style: { padding: 14 }, children: [_jsx("div", { children: _jsx("strong", { children: status.running ? `${status.phase}…` : status.error ? 'The import stopped' : 'Import finished' }) }), _jsxs("div", { className: "muted small", children: [status.sources, " playlists and guides added \u00B7 ", status.channels, " channels created", status.existing > 0 && ` · ${status.existing} channels were already here`] }), status.error && _jsx("div", { className: "form-error small", children: status.error }), status.notes.length > 0 && (_jsx("ul", { className: "small", style: { margin: '8px 0 0', paddingLeft: 18 }, children: status.notes.map((n, i) => _jsx("li", { children: n }, i)) }))] })), !plan && !running && (_jsxs("form", { className: "form", onSubmit: connect, children: [_jsx("p", { className: "muted small", style: { margin: 0 }, children: "Copies your provider accounts (with connection limits and user agents), guides, and channels with their numbers, groups, logos, guide matches and fallback streams. BingeBandit then connects to your providers itself. Your login is only used for this import and isn't saved. Running it again skips what's already here." }), _jsxs("label", { children: ["Dispatcharr address", _jsx("input", { className: "mono", value: form.url, onChange: set('url'), required: true, placeholder: "http://192.168.1.10:9191" })] }), _jsxs("div", { className: "segmented", children: [_jsx("button", { type: "button", className: !useKey ? 'active' : '', onClick: () => setUseKey(false), children: "Username and password" }), _jsx("button", { type: "button", className: useKey ? 'active' : '', onClick: () => setUseKey(true), children: "API key" })] }), useKey ? (_jsxs("label", { children: ["API key (an admin's)", _jsx("input", { className: "mono", value: form.apiKey, onChange: set('apiKey'), required: true })] })) : (_jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Admin username", _jsx("input", { value: form.username, onChange: set('username'), required: true, autoComplete: "off" })] }), _jsxs("label", { children: ["Password", _jsx("input", { type: "password", value: form.password, onChange: set('password'), required: true, autoComplete: "new-password" })] })] })), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Close" }), _jsx("button", { className: "btn btn-primary", disabled: busy, children: busy ? 'Connecting…' : 'Connect' })] })] })), plan && !running && (_jsxs(_Fragment, { children: [_jsx("h3", { style: { margin: 0 }, children: "This will be imported" }), _jsxs("div", { className: "panel list", children: [plan.accounts.map((a) => _jsx(PlanRow, { item: a }, `a${a.name}`)), plan.guides.map((g) => _jsx(PlanRow, { item: g }, `g${g.name}`)), _jsx("div", { className: "list-row", children: _jsxs("div", { className: "grow", children: [_jsxs("div", { children: [plan.channels.toLocaleString(), " channels"] }), _jsxs("div", { className: "muted small", children: ["in ", plan.groups, " ", plan.groups === 1 ? 'group' : 'groups', ", with their numbers, logos, guide matches and fallback streams"] })] }) })] }), _jsx("p", { className: "muted small", style: { margin: 0 }, children: "Playlists and guides are read first, which can take a few minutes for big providers. Channels whose streams can't be found are skipped and listed when it's done." }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { className: "btn btn-glass", onClick: () => setPlan(null), children: "Back" }), _jsx("button", { className: "btn btn-primary", disabled: busy, onClick: start, children: busy ? 'Starting…' : 'Import' })] })] })), running && (_jsxs("div", { className: "actions end", children: [_jsx("span", { className: "muted small grow", children: "You can close this; the import keeps going." }), _jsx("button", { className: "btn btn-glass", onClick: onClose, children: "Close" })] }))] }) }));
}
function PlanRow({ item }) {
    return (_jsx("div", { className: "list-row", children: _jsxs("div", { className: "grow", children: [_jsxs("div", { children: [item.name, " ", item.skip && _jsx("span", { className: "pill", children: "Skipped" })] }), _jsx("div", { className: "muted small", children: item.skip || item.detail })] }) }));
}
