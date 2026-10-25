import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { Icon } from '../components/Icon';
import { useApi, useDocumentTitle } from '../hooks';
import { formatBytes } from '../format';
import { clock } from './LiveGuide';
const keepChoices = [0, 1, 2, 3, 5, 7, 10, 14, 20, 30];
const day = (unix) => new Date(unix * 1000).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' });
const statusPill = {
    pending: ['warn', 'Needs approval'],
    scheduled: ['', 'Scheduled'],
    recording: ['danger', 'Recording'],
    completed: ['ok', 'Recorded'],
    failed: ['danger', 'Failed'],
    cancelled: ['', 'Cancelled'],
};
export default function RecordingsPage() {
    useDocumentTitle('Recordings');
    const { user } = useAuth();
    const { data, reload } = useApi('/api/dvr', 10000);
    if (!data)
        return _jsx("div", { className: "page-loading" });
    const upcoming = data.recordings.filter((r) => ['pending', 'scheduled', 'recording'].includes(r.status));
    const done = data.recordings.filter((r) => !['pending', 'scheduled', 'recording'].includes(r.status));
    const remove = async (r, withFile = false) => {
        const what = r.status === 'recording' ? 'Stop this recording? What was recorded so far is kept.' : withFile ? `Delete "${r.title}" and its file?` : null;
        if (what && !confirm(what))
            return;
        await api.del(`/api/dvr/recordings/${r.id}${withFile ? '?file=1' : ''}`).catch((e) => alert(e.message));
        reload();
    };
    const approve = async (path) => {
        await api.post(path).catch((e) => alert(e.message));
        reload();
    };
    const row = (r) => {
        const [cls, label] = statusPill[r.status];
        return (_jsxs("div", { className: "list-row rec-row", children: [_jsxs("div", { className: "rec-when", children: [_jsx("div", { children: day(r.programStart) }), _jsxs("div", { className: "muted small", children: [clock(r.programStart), "\u2013", clock(r.programStop)] })] }), _jsxs("div", { className: "grow", children: [_jsxs("div", { children: [r.status === 'recording' && _jsx("span", { className: "rec-dot live" }), " ", r.title, r.subtitle && _jsxs("span", { className: "muted", children: [" \u00B7 ", r.subtitle] })] }), _jsx("div", { className: "muted small", children: [r.channelName, r.ruleId ? 'series' : '', user?.isAdmin && r.username, r.size ? formatBytes(r.size) : '', r.error].filter(Boolean).join(' · ') }), r.conflict && _jsxs("div", { className: "small rec-conflict", children: ["May not record: ", r.conflict] })] }), r.conflict && r.status !== 'recording' ? _jsx("span", { className: "pill warn", children: "Conflict" }) : _jsx("span", { className: `pill ${cls}`, children: label }), r.status === 'pending' && user?.isAdmin && (_jsx("button", { className: "btn btn-primary btn-sm", onClick: () => approve(`/api/admin/dvr/recordings/${r.id}/approve`), children: "Approve" })), r.status === 'completed' && r.itemId ? (_jsxs(Link, { className: "btn btn-glass btn-sm", to: `/play/${r.itemId}`, children: [_jsx(Icon, { name: "play", fill: true, size: 14 }), " Watch"] })) : null, ['pending', 'scheduled', 'recording'].includes(r.status) ? (_jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(r), children: r.status === 'recording' ? 'Stop' : 'Cancel' })) : (_jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(r, true), "aria-label": "Delete recording", children: _jsx(Icon, { name: "trash", size: 16 }) }))] }, r.id));
    };
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { children: [_jsx("h1", { children: "Recordings" }), _jsx("div", { className: "muted", children: "Record from the Live TV guide: pick a program, then Record or Record series." })] }), _jsxs(Link, { to: "/live", className: "btn btn-glass", children: [_jsx(Icon, { name: "broadcast", size: 18 }), " Open the guide"] })] }), !data.canRecord && _jsx("div", { className: "notice", children: "Recording is turned off for your account. Ask your server admin." }), _jsxs("h2", { className: "section-title", children: ["Upcoming ", _jsx("span", { className: "count", children: upcoming.length })] }), _jsxs("div", { className: "panel list", children: [upcoming.map(row), upcoming.length === 0 && _jsx("div", { className: "list-row muted", children: "Nothing scheduled." })] }), _jsxs("h2", { className: "section-title", children: ["Series ", _jsx("span", { className: "count", children: data.rules.length })] }), _jsxs("div", { className: "panel list", children: [data.rules.map((rule) => (_jsxs("div", { className: "list-row", children: [_jsx(Icon, { name: "list" }), _jsxs("div", { className: "grow", children: [_jsx("div", { children: rule.title }), _jsx("div", { className: "muted small", children: [rule.channelName || 'any channel', `${rule.upcoming} upcoming`, user?.isAdmin && rule.username].filter(Boolean).join(' · ') })] }), _jsxs("label", { className: "rec-keep", children: [_jsx("span", { className: "muted small", children: "Keep" }), _jsx("select", { value: rule.keepLast, onChange: async (e) => {
                                            const keep = Number(e.target.value);
                                            if (keep > 0 && !confirm(`Keep only the newest ${keep} recording${keep === 1 ? '' : 's'} of "${rule.title}"? Older ones and their files are deleted.`))
                                                return;
                                            await api.patch(`/api/dvr/rules/${rule.id}`, { keepLast: keep }).catch((err) => alert(err.message));
                                            reload();
                                        }, children: [...new Set([...keepChoices, rule.keepLast])].sort((a, b) => a - b).map((n) => (_jsx("option", { value: n, children: n === 0 ? 'All' : `Newest ${n}` }, n))) })] }), rule.status === 'pending' && _jsx("span", { className: "pill warn", children: "Needs approval" }), rule.status === 'pending' && user?.isAdmin && (_jsx("button", { className: "btn btn-primary btn-sm", onClick: () => approve(`/api/admin/dvr/rules/${rule.id}/approve`), children: "Approve" })), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: async () => {
                                    if (!confirm(`Stop recording "${rule.title}"? Scheduled episodes are cancelled; finished ones are kept.`))
                                        return;
                                    await api.del(`/api/dvr/rules/${rule.id}`);
                                    reload();
                                }, children: "Stop series" })] }, rule.id))), data.rules.length === 0 && _jsx("div", { className: "list-row muted", children: "No series recordings." })] }), _jsxs("h2", { className: "section-title", children: ["Recorded ", _jsx("span", { className: "count", children: done.length })] }), _jsxs("div", { className: "panel list", children: [done.map(row), done.length === 0 && _jsx("div", { className: "list-row muted", children: "No recordings yet." })] }), user?.isAdmin && (_jsxs("p", { className: "muted small", style: { marginTop: 14 }, children: ["Recordings are saved to ", _jsx("span", { className: "mono", children: data.folder }), ". Add that folder as a TV Shows library to browse and watch them with artwork; Watch buttons appear here once the library has picked them up."] }))] }));
}
