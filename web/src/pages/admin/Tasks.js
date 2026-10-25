import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { api, uploadBackup } from '../../api';
import { Icon } from '../../components/Icon';
import { useApi, useDocumentTitle } from '../../hooks';
import { formatBytes, timeAgo } from '../../format';
// [setting, label, placeholder, full width]
const remoteFields = [
    ['backup_s3_endpoint', 'Endpoint', 'https://s3.us-west-002.backblazeb2.com', true],
    ['backup_s3_bucket', 'Bucket', 'my-backups', false],
    ['backup_s3_region', 'Region', 'us-west-002 (blank = us-east-1)', false],
    ['backup_s3_prefix', 'Folder in the bucket', 'bingebandit', false],
    ['backup_s3_access_key', 'Access key ID (B2: key ID)', '', false],
    ['backup_s3_secret_key', 'Secret access key (B2: application key)', '', true],
];
const intervals = [
    [1, 'Every hour'],
    [3, 'Every 3 hours'],
    [6, 'Every 6 hours'],
    [12, 'Every 12 hours'],
    [24, 'Daily'],
    [72, 'Every 3 days'],
    [168, 'Weekly'],
    [336, 'Every 2 weeks'],
    [720, 'Monthly'],
];
const hours = Array.from({ length: 24 }, (_, h) => h);
const hourLabel = (h) => new Date(2000, 0, 1, h).toLocaleTimeString([], { hour: 'numeric' });
function duration(ms) {
    if (ms < 1000)
        return `${ms} ms`;
    const s = Math.round(ms / 1000);
    return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}
function when(unix) {
    const diff = unix - Date.now() / 1000;
    if (diff < 60)
        return 'soon';
    if (diff < 3600)
        return `in ${Math.round(diff / 60)}m`;
    if (diff < 86400)
        return `in ${Math.round(diff / 3600)}h`;
    return `in ${Math.round(diff / 86400)}d`;
}
export default function AdminTasks() {
    useDocumentTitle('Scheduled Tasks');
    const { data: tasks, reload } = useApi('/api/admin/tasks', 3000);
    const { data: settings } = useApi('/api/admin/settings');
    const [win, setWin] = useState({ start: '2', end: '5', dir: '', keep: '7' });
    const [msg, setMsg] = useState(null);
    const [remote, setRemote] = useState({});
    const [testing, setTesting] = useState(false);
    useEffect(() => {
        if (!settings)
            return;
        setWin({ start: settings.maintenance_start, end: settings.maintenance_end, dir: settings.backup_dir, keep: settings.backup_keep });
        setRemote(Object.fromEntries(remoteFields.map(([k]) => [k, settings[k] ?? ''])));
    }, [settings]);
    const configure = async (t, enabled, intervalHours) => {
        await api.put(`/api/admin/tasks/${t.id}`, { enabled, intervalHours }).catch((e) => alert(e.message));
        reload();
    };
    const run = async (t) => {
        await api.post(`/api/admin/tasks/${t.id}/run`).catch((e) => alert(e.message));
        reload();
    };
    const saveSettings = async () => {
        try {
            await api.put('/api/admin/settings', {
                maintenance_start: win.start,
                maintenance_end: win.end,
                backup_dir: win.dir,
                backup_keep: win.keep,
                ...remote,
            });
            setMsg({ ok: true, text: 'Saved.' });
        }
        catch (e) {
            setMsg({ ok: false, text: e.message });
        }
    };
    const testRemote = async () => {
        setTesting(true);
        setMsg(null);
        try {
            const res = await api.post('/api/admin/backups/remote/test', remote);
            setMsg({ ok: true, text: `Connected: BingeBandit can upload, list and delete there (${res.backups} backups found). Save to use it.` });
        }
        catch (e) {
            setMsg({ ok: false, text: e.message });
        }
        setTesting(false);
    };
    return (_jsxs("div", { className: "page", children: [_jsx("header", { className: "page-head", children: _jsxs("div", { children: [_jsx("h1", { children: "Scheduled Tasks" }), _jsx("div", { className: "muted", children: "Background jobs that keep the server tidy. Times use the server's clock." })] }) }), _jsx("div", { className: "panel list", children: tasks?.map((t) => (_jsxs("div", { className: "task-row", children: [_jsxs("label", { className: "switch", title: t.enabled ? 'Enabled' : 'Disabled', children: [_jsx("input", { type: "checkbox", checked: t.enabled, onChange: (e) => configure(t, e.target.checked, t.intervalHours) }), _jsx("span", {})] }), _jsxs("div", { className: "grow", children: [_jsxs("div", { className: "task-name", children: [t.name, t.maintenance && _jsx("span", { className: "pill", children: "Maintenance window" })] }), _jsx("div", { className: "muted small", children: t.description }), _jsxs("div", { className: "task-meta small", children: [t.running ? (_jsxs("span", { className: "accent", children: [_jsx(Icon, { name: "refresh", size: 12, className: "spin" }), " Running\u2026"] })) : t.lastRun ? (_jsxs("span", { className: t.lastError ? 'task-error' : 'muted', children: ["Last run ", timeAgo(t.lastRun), " \u00B7 ", duration(t.lastDurationMs), " \u00B7 ", t.lastError || t.lastResult] })) : (_jsx("span", { className: "muted", children: "Never run" })), t.enabled && !t.running && _jsxs("span", { className: "muted", children: [" \u00B7 next ", t.maintenance ? 'in the window, ' : '', when(t.nextRun)] })] })] }), _jsxs("select", { value: t.intervalHours, disabled: !t.enabled, onChange: (e) => configure(t, t.enabled, Number(e.target.value)), children: [!intervals.some(([h]) => h === t.intervalHours) && _jsxs("option", { value: t.intervalHours, children: ["Every ", t.intervalHours, " hours"] }), intervals.map(([h, label]) => (_jsx("option", { value: h, children: label }, h)))] }), _jsxs("button", { className: "btn btn-glass btn-sm", disabled: t.running, onClick: () => run(t), children: [_jsx(Icon, { name: "play", fill: true, size: 14 }), " Run now"] })] }, t.id))) }), _jsx("h2", { className: "section-title", children: "Maintenance window and backups" }), _jsxs("div", { className: "panel form", style: { maxWidth: 760 }, children: [_jsxs("div", { className: "field-grid", children: [_jsxs("label", { children: ["Window starts", _jsx("select", { value: win.start, onChange: (e) => setWin({ ...win, start: e.target.value }), children: hours.map((h) => _jsx("option", { value: h, children: hourLabel(h) }, h)) })] }), _jsxs("label", { children: ["Window ends", _jsx("select", { value: win.end, onChange: (e) => setWin({ ...win, end: e.target.value }), children: hours.map((h) => _jsx("option", { value: h, children: hourLabel(h) }, h)) })] }), _jsxs("label", { className: "span-2", children: ["Backup folder (use a NAS share or another drive for off-machine backups)", _jsx("input", { className: "mono", value: win.dir, onChange: (e) => setWin({ ...win, dir: e.target.value }), placeholder: "Default: backups inside the data folder" })] }), _jsxs("label", { children: ["Backups to keep", _jsx("input", { type: "number", min: 1, max: 365, value: win.keep, onChange: (e) => setWin({ ...win, keep: e.target.value }) })] })] }), _jsx("p", { className: "muted small", children: "Maintenance tasks only start inside the window. Set the same start and end hour to allow any time." }), _jsx("h3", { children: "Also upload to cloud storage" }), _jsx("p", { className: "muted small", children: "Any S3-compatible service: Amazon S3, Backblaze B2, Wasabi, Cloudflare R2 or your own MinIO. Each backup is uploaded after it's saved, and the same number of backups is kept there. Leave the endpoint empty to turn this off." }), _jsx("div", { className: "field-grid", children: remoteFields.map(([key, label, placeholder, span]) => (_jsxs("label", { className: span ? 'span-2' : undefined, children: [label, _jsx("input", { className: "mono", type: key === 'backup_s3_secret_key' ? 'password' : 'text', autoComplete: "off", value: remote[key] ?? '', placeholder: placeholder, onChange: (e) => setRemote({ ...remote, [key]: e.target.value }) })] }, key))) }), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text }), _jsxs("div", { className: "actions", children: [_jsx("button", { className: "btn btn-primary", onClick: saveSettings, children: "Save" }), remote.backup_s3_endpoint && (_jsx("button", { className: "btn btn-glass", disabled: testing, onClick: testRemote, children: testing ? 'Testing…' : 'Test cloud storage' }))] })] }), _jsx(Restore, {})] }));
}
function Restore() {
    const { data, reload } = useApi('/api/admin/backups');
    const [busy, setBusy] = useState('');
    const [msg, setMsg] = useState(null);
    const restore = async (b) => {
        const when = new Date(b.modified * 1000).toLocaleString();
        if (!confirm(`Restore the backup from ${when}? Users, watch history, settings and edits go back to how they were then. The current database is backed up first.`))
            return;
        setBusy(b.name);
        setMsg(null);
        try {
            const res = await api.post(`/api/admin/backups/${b.name}/restore`);
            setMsg({
                ok: true,
                text: `Restored. The data from before is saved as ${res.previous}. Restart the server so folder watching and Live TV pick up the restored settings.`,
            });
        }
        catch (e) {
            setMsg({ ok: false, text: e.message });
        }
        setBusy('');
        reload();
    };
    const restoreRemote = async (b) => {
        const when = new Date(b.modified * 1000).toLocaleString();
        if (!confirm(`Download and restore the cloud backup from ${when}? Users, watch history, settings and edits go back to how they were then. The current database is backed up first.`))
            return;
        setBusy('remote:' + b.name);
        setMsg(null);
        try {
            const res = await api.post(`/api/admin/backups/remote/${b.name}/restore`);
            setMsg({ ok: true, text: `Restored ${res.restored}. The data from before is saved as ${res.previous}. Restart the server so folder watching and Live TV pick up the restored settings.` });
        }
        catch (e) {
            setMsg({ ok: false, text: e.message });
        }
        setBusy('');
        reload();
    };
    const upload = async (file) => {
        if (!confirm(`Restore from ${file.name}? Users, watch history, settings and edits are replaced by the ones in the file. The current database is backed up first.`))
            return;
        setBusy('upload');
        setMsg(null);
        try {
            const res = await uploadBackup('/api/admin/backups/upload', file);
            setMsg({
                ok: true,
                text: `Restored from ${file.name}. The data from before is saved as ${res.previous}. If the backup came from another server, check the library folders in Settings, then restart the server.`,
            });
        }
        catch (e) {
            setMsg({ ok: false, text: e.message });
        }
        setBusy('');
        reload();
    };
    return (_jsxs(_Fragment, { children: [_jsx("h2", { className: "section-title", children: "Restore a backup" }), _jsxs("div", { className: "panel list", style: { maxWidth: 760 }, children: [data && data.backups.length === 0 && (_jsx("div", { className: "task-row muted", children: "No backups yet. Run \"Back up database\" above to make one." })), data?.backups.map((b) => (_jsxs("div", { className: "task-row", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { className: "task-name", children: new Date(b.modified * 1000).toLocaleString() }), _jsxs("div", { className: "muted small", children: [b.name, " \u00B7 ", formatBytes(b.size), " \u00B7 ", timeAgo(b.modified)] })] }), _jsxs("a", { className: "btn btn-glass btn-sm", href: `/api/admin/backups/${b.name}/download`, download: true, title: "Download, to keep elsewhere or move to a new server", children: [_jsx(Icon, { name: "download", size: 14 }), " Download"] }), _jsxs("button", { className: "btn btn-glass btn-sm danger", disabled: !!busy, onClick: () => restore(b), children: [_jsx(Icon, { name: "refresh", size: 14, className: busy === b.name ? 'spin' : '' }), " Restore"] })] }, b.name))), _jsxs("div", { className: "task-row", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { className: "task-name", children: "Restore from a file" }), _jsx("div", { className: "muted small", children: "A backup downloaded from this or another BingeBandit server (a .db file)" })] }), _jsxs("label", { className: `btn btn-glass btn-sm danger${busy ? ' disabled' : ''}`, children: [_jsx(Icon, { name: "upload", size: 14, className: busy === 'upload' ? 'spin' : '' }), " ", busy === 'upload' ? 'Uploading…' : 'Upload', _jsx("input", { type: "file", accept: ".db,application/vnd.sqlite3,application/x-sqlite3", hidden: true, disabled: !!busy, onChange: (e) => {
                                            const f = e.target.files?.[0];
                                            e.target.value = '';
                                            if (f)
                                                upload(f);
                                        } })] })] })] }), data && _jsxs("p", { className: "muted small", children: ["Backups in ", _jsx("span", { className: "mono", children: data.dir })] }), data?.bucket && (_jsxs(_Fragment, { children: [_jsxs("h3", { children: ["In cloud storage (", data.bucket, ")"] }), _jsxs("div", { className: "panel list", style: { maxWidth: 760 }, children: [data.remoteError && _jsxs("div", { className: "task-row form-error", children: ["Couldn't list the bucket: ", data.remoteError] }), !data.remoteError && data.remote?.length === 0 && _jsx("div", { className: "task-row muted", children: "Nothing uploaded yet." }), data.remote?.map((b) => (_jsxs("div", { className: "task-row", children: [_jsxs("div", { className: "grow", children: [_jsx("div", { className: "task-name", children: new Date(b.modified * 1000).toLocaleString() }), _jsxs("div", { className: "muted small", children: [b.name, " \u00B7 ", formatBytes(b.size), " \u00B7 ", timeAgo(b.modified)] })] }), _jsxs("button", { className: "btn btn-glass btn-sm danger", disabled: !!busy, onClick: () => restoreRemote(b), children: [_jsx(Icon, { name: "refresh", size: 14, className: busy === 'remote:' + b.name ? 'spin' : '' }), " Restore"] })] }, b.name)))] })] })), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', style: { maxWidth: 760 }, children: msg.text })] }));
}
