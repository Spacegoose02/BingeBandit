import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../../api';
import { Icon } from '../../components/Icon';
import { Modal } from '../../components/Modal';
import { useApi, useDocumentTitle } from '../../hooks';
const stateLabels = {
    running: ['Running', 'ok'],
    starting: ['Starting', 'warn'],
    stopped: ['Off', ''],
    error: ['Error', 'danger'],
    missing: ['Folder missing', 'danger'],
};
export default function AdminPlugins() {
    useDocumentTitle('Plugins');
    const { data, reload } = useApi('/api/admin/plugins', 5000);
    const [adding, setAdding] = useState(false);
    const [editing, setEditing] = useState(null);
    const [busy, setBusy] = useState('');
    const act = async (p, fn) => {
        setBusy(p.id);
        await fn().catch((e) => alert(e.message));
        setBusy('');
        reload();
    };
    const toggle = (p, enabled) => act(p, () => api.put(`/api/admin/plugins/${p.id}`, { enabled }));
    const restart = (p) => act(p, () => api.post(`/api/admin/plugins/${p.id}/restart`));
    const uninstall = (p) => {
        if (!confirm(`Uninstall ${p.manifest.name}? Its folder and settings are deleted.`))
            return;
        act(p, () => api.post(`/api/admin/plugins/${p.id}/uninstall`));
    };
    const remove = (p) => {
        if (!confirm(`Remove ${p.manifest.name}? Its settings are deleted too.`))
            return;
        act(p, () => api.del(`/api/admin/plugins/${p.id}`));
    };
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { children: [_jsx("h1", { children: "Plugins" }), _jsx("div", { className: "muted", children: "Add-ons that run alongside BingeBandit and talk to it over its plugin API." })] }), _jsxs("button", { className: "btn btn-glass", onClick: () => setAdding(true), children: [_jsx(Icon, { name: "plus", size: 16 }), " Add by address"] })] }), _jsxs("div", { className: "panel list", children: [data?.plugins.length === 0 && (_jsx("p", { className: "muted", style: { padding: 12 }, children: "No plugins yet. Put a plugin's folder in one of the plugins folders below, or add one that runs as its own service." })), data?.plugins.map((p) => {
                        const [label, tone] = stateLabels[p.state] ?? [p.state, ''];
                        const m = p.manifest;
                        return (_jsxs("div", { className: "task-row", children: [_jsxs("label", { className: "switch", title: p.enabled ? 'On' : 'Off', children: [_jsx("input", { type: "checkbox", checked: p.enabled, disabled: busy === p.id, onChange: (e) => toggle(p, e.target.checked), "aria-label": `${m.name} on` }), _jsx("span", {})] }), _jsxs("div", { className: "grow", children: [_jsxs("div", { className: "task-name", children: [m.name, m.version && _jsxs("span", { className: "muted small", children: [" ", m.version] }), _jsx("span", { className: `pill ${tone}`, children: label }), _jsx("span", { className: `pill ${p.kind === 'remote' ? 'remote' : ''}`, children: p.kind === 'remote' ? 'Own service' : 'Plugins folder' })] }), m.description && _jsx("div", { className: "muted small", children: m.description }), p.kind === 'remote' && _jsx("div", { className: "muted small mono", style: { wordBreak: 'break-all' }, children: p.url }), p.error && _jsx("div", { className: "task-error small", children: p.error }), p.state === 'running' && !!m.pages?.length && (_jsx("div", { className: "small", style: { marginTop: 4, display: 'flex', gap: 12, flexWrap: 'wrap' }, children: m.pages.map((pg) => (_jsxs("a", { className: "accent", href: `/api/plugins/${p.id}${pg.path.startsWith('/') ? '' : '/'}${pg.path}`, target: "_blank", rel: "noreferrer", children: [pg.title, " \u2197"] }, pg.path))) }))] }), !!m.settings?.length && (_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing(p), children: [_jsx(Icon, { name: "sliders", size: 14 }), " Settings"] })), p.enabled && (_jsxs("button", { className: "btn btn-glass btn-sm", disabled: busy === p.id, onClick: () => restart(p), children: [_jsx(Icon, { name: "refresh", size: 14 }), " ", p.kind === 'remote' ? 'Check' : 'Restart'] })), p.installed && (_jsxs("button", { className: "btn btn-glass btn-sm danger", disabled: busy === p.id, onClick: () => uninstall(p), children: [_jsx(Icon, { name: "trash", size: 14 }), " Uninstall"] })), (p.kind === 'remote' || p.state === 'missing') && (_jsx("button", { className: "btn btn-glass btn-sm danger", disabled: busy === p.id, onClick: () => remove(p), "aria-label": `Remove ${m.name}`, children: _jsx(Icon, { name: "trash", size: 16 }) }))] }, p.id));
                    })] }), _jsx(Catalogue, { onChanged: reload }), data && (_jsxs("p", { className: "muted small", style: { marginTop: 12 }, children: ["Plugins folders: ", data.dirs.filter(Boolean).map((d, i) => _jsxs("span", { children: [i > 0 && ' · ', _jsx("code", { children: d })] }, d)), ". Each plugin is a folder with a ", _jsx("code", { children: "plugin.json" }), "; new ones show up within a minute and start when you turn them on. Only add plugins you trust: a plugin can read and change every user's library data, and its pages open inside BingeBandit."] })), adding && _jsx(AddPlugin, { onClose: () => setAdding(false), onAdded: () => { setAdding(false); reload(); } }), editing && _jsx(PluginSettings, { plugin: editing, onClose: () => setEditing(null), onSaved: () => { setEditing(null); reload(); } })] }));
}
function AddPlugin({ onClose, onAdded }) {
    const [url, setUrl] = useState('');
    const [serverUrl, setServerUrl] = useState(window.location.origin);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const submit = async (e) => {
        e.preventDefault();
        setBusy(true);
        setError('');
        try {
            await api.post('/api/admin/plugins', { url, serverUrl });
            onAdded();
        }
        catch (err) {
            setError(err.message);
        }
        setBusy(false);
    };
    return (_jsx(Modal, { title: "Add a plugin by address", onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: submit, children: [_jsx("p", { className: "muted small", children: "For a plugin that runs as its own service, such as its own Docker container. BingeBandit reads its manifest and pairs with it." }), _jsxs("label", { children: ["Plugin address", _jsx("input", { className: "mono", type: "url", value: url, onChange: (e) => setUrl(e.target.value), required: true, autoFocus: true, placeholder: "http://192.168.1.20:8490" })] }), _jsxs("label", { children: ["BingeBandit's address, as the plugin reaches it", _jsx("input", { className: "mono", type: "url", value: serverUrl, onChange: (e) => setServerUrl(e.target.value), required: true })] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", disabled: busy, children: busy ? 'Pairing…' : 'Add' })] })] }) }));
}
function PluginSettings({ plugin, onClose, onSaved }) {
    const fields = plugin.manifest.settings ?? [];
    const [values, setValues] = useState(plugin.values);
    const [error, setError] = useState('');
    const set = (k, v) => setValues({ ...values, [k]: v });
    const save = async (e) => {
        e.preventDefault();
        try {
            await api.put(`/api/admin/plugins/${plugin.id}/settings`, values);
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: `${plugin.manifest.name} settings`, onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [fields.map((f) => f.type === 'bool' ? (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: (values[f.key] ?? f.default) === 'true', onChange: (e) => set(f.key, String(e.target.checked)) }), f.label, f.help && _jsxs("span", { className: "muted small", children: [" ", f.help] })] }, f.key)) : (_jsxs("label", { children: [f.label, f.type === 'select' ? (_jsx("select", { value: values[f.key] ?? f.default ?? '', onChange: (e) => set(f.key, e.target.value), children: f.options?.map((o) => _jsx("option", { value: o, children: o }, o)) })) : (_jsx("input", { type: f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text', value: values[f.key] ?? '', placeholder: f.type === 'password' && plugin.secretsSet.includes(f.key) ? 'Saved; leave empty to keep' : f.default, autoComplete: "off", onChange: (e) => set(f.key, e.target.value) })), f.help && _jsx("span", { className: "muted small", children: f.help })] }, f.key))), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: "Save" })] })] }) }));
}
function Catalogue({ onChanged }) {
    const [refresh, setRefresh] = useState(0);
    const { data, reload } = useApi(`/api/admin/plugins/catalog${refresh ? `?refresh=${refresh}` : ''}`);
    const [busy, setBusy] = useState('');
    const [editing, setEditing] = useState(false);
    const install = async (c) => {
        if (!c.installed && !confirm(`Install ${c.name} ${c.version}? It runs as a program on this server, with access to every user's library data.`))
            return;
        setBusy(c.id);
        try {
            await api.post('/api/admin/plugins/catalog/install', { catalog: c.catalog, id: c.id });
            await reload();
            onChanged();
        }
        catch (e) {
            alert(e.message);
        }
        setBusy('');
    };
    const source = (url) => {
        if (url === data?.official)
            return 'BingeBandit';
        try {
            return new URL(url).host;
        }
        catch {
            return url;
        }
    };
    return (_jsxs(_Fragment, { children: [_jsxs("div", { className: "section-title-row", children: [_jsx("h2", { className: "section-title", children: "Catalogue" }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing(true), children: [_jsx(Icon, { name: "list", size: 14 }), " Catalogues"] }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => setRefresh(Date.now()), "aria-label": "Check the catalogues again", children: _jsx(Icon, { name: "refresh", size: 14 }) })] })] }), _jsxs("div", { className: "panel list", children: [!data && _jsx("p", { className: "muted", style: { padding: 12 }, children: "Loading\u2026" }), data?.errors.map((e) => (_jsxs("div", { className: `${e.url === data.official ? 'muted' : 'task-error'} small`, style: { padding: '8px 12px' }, children: [source(e.url), ": ", e.error] }, e.url))), data && data.plugins.length === 0 && data.errors.length === 0 && _jsx("p", { className: "muted", style: { padding: 12 }, children: "The catalogues list no plugins." }), data?.plugins.map((c) => (_jsxs("div", { className: "task-row", children: [_jsxs("div", { className: "grow", children: [_jsxs("div", { className: "task-name", children: [c.name, _jsxs("span", { className: "muted small", children: [" ", c.version] }), c.installed === 'installed' && _jsxs("span", { className: "pill ok", children: ["Installed", c.current && c.update ? ` ${c.current}` : ''] }), c.installed === 'bundled' && _jsxs("span", { className: "pill", children: ["Comes with the server", c.current ? ` (${c.current})` : ''] }), data.catalogs.length > 1 && _jsx("span", { className: "pill remote", children: source(c.catalog) })] }), c.description && _jsx("div", { className: "muted small", children: c.description }), _jsxs("div", { className: "muted small", children: [c.author && _jsxs(_Fragment, { children: ["By ", c.author] }), c.author && c.homepage && ' · ', c.homepage && (_jsx("a", { className: "accent", href: c.homepage, target: "_blank", rel: "noreferrer", children: "About \u2197" }))] })] }), !c.available ? (_jsxs("span", { className: "muted small", children: ["Not built for ", data.platform] })) : !c.installed ? (_jsxs("button", { className: "btn btn-primary btn-sm", disabled: !!busy, onClick: () => install(c), children: [_jsx(Icon, { name: "download", size: 14 }), " ", busy === c.id ? 'Installing…' : 'Install'] })) : c.update ? (_jsxs("button", { className: "btn btn-glass btn-sm", disabled: !!busy, onClick: () => install(c), children: [_jsx(Icon, { name: "download", size: 14 }), " ", busy === c.id ? 'Updating…' : `Update to ${c.version}`] })) : null] }, c.id)))] }), data && (_jsxs("p", { className: "muted small", style: { marginTop: 8 }, children: ["Installs go into ", _jsx("code", { children: data.installDir }), " and start turned off. A newer copy there replaces one that comes with the server."] })), editing && data && (_jsx(CatalogueList, { official: data.official, current: data.catalogs.filter((u) => u !== data.official), onClose: () => setEditing(false), onSaved: () => {
                    setEditing(false);
                    setRefresh(Date.now());
                } }))] }));
}
function CatalogueList({ official, current, onClose, onSaved }) {
    const [text, setText] = useState(current.join('\n'));
    const [error, setError] = useState('');
    const save = async (e) => {
        e.preventDefault();
        try {
            await api.put('/api/admin/settings', { plugin_catalogs: text });
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: "Plugin catalogues", onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("p", { className: "muted small", children: ["BingeBandit's own catalogue is always used (", _jsx("code", { style: { wordBreak: 'break-all' }, children: official }), "). Add the addresses of other catalogues here, one per line. Only add catalogues you trust: their plugins run as programs on this server."] }), _jsxs("label", { children: ["Other catalogues", _jsx("textarea", { className: "mono", rows: 4, value: text, onChange: (e) => setText(e.target.value), placeholder: "https://example.com/plugin-catalog.json" })] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: "Save" })] })] }) }));
}
