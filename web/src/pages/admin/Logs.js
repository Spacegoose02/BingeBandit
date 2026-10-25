import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../../api';
import { Icon } from '../../components/Icon';
import { useDocumentTitle } from '../../hooks';
const MAX_LINES = 3000;
/** Lines from the live TV relay, custom channels and the DVR. */
const LIVE_LINE = /\b(live tv:|custom channel |dvr:)/i;
export default function AdminLogs() {
    useDocumentTitle('Logs');
    const [lines, setLines] = useState([]);
    const [filter, setFilter] = useState('');
    const [level, setLevel] = useState('all');
    const [params, setParams] = useSearchParams();
    const liveOnly = params.get('only') === 'live';
    const setLiveOnly = (on) => setParams(on ? { only: 'live' } : {}, { replace: true });
    const [follow, setFollow] = useState(true);
    const lastSeq = useRef(0);
    const box = useRef(null);
    useEffect(() => {
        let stop = false;
        const poll = async () => {
            try {
                const fresh = await api.get(`/api/admin/logs?after=${lastSeq.current}`);
                if (!stop && fresh.length) {
                    lastSeq.current = fresh[fresh.length - 1].seq;
                    setLines((prev) => [...prev, ...fresh].slice(-MAX_LINES));
                }
            }
            catch {
                // keep polling; the server may be restarting
            }
        };
        poll();
        const t = window.setInterval(poll, 2000);
        return () => {
            stop = true;
            window.clearInterval(t);
        };
    }, []);
    useEffect(() => {
        if (follow && box.current)
            box.current.scrollTop = box.current.scrollHeight;
    }, [lines, follow, filter, level, liveOnly]);
    const needle = filter.toLowerCase();
    const shown = lines.filter((l) => (!liveOnly || LIVE_LINE.test(l.text)) && (level === 'all' || l.level === 'error' || (level === 'warn' && l.level === 'warn')) && (!needle || l.text.toLowerCase().includes(needle)));
    return (_jsxs("div", { className: "page logs-page", children: [_jsxs("header", { className: "page-head", children: [_jsx("h1", { children: "Logs" }), _jsxs("div", { className: "actions", children: [_jsxs("div", { className: "segmented", children: [_jsx("button", { className: liveOnly ? '' : 'active', onClick: () => setLiveOnly(false), children: "Everything" }), _jsx("button", { className: liveOnly ? 'active' : '', onClick: () => setLiveOnly(true), children: "Live TV" })] }), _jsx("input", { className: "log-filter", value: filter, onChange: (e) => setFilter(e.target.value), placeholder: "Filter" }), _jsx("div", { className: "segmented", children: ['all', 'warn', 'error'].map((l) => (_jsx("button", { className: level === l ? 'active' : '', onClick: () => setLevel(l), children: l === 'all' ? 'All' : l === 'warn' ? 'Warnings+' : 'Errors' }, l))) }), _jsxs("label", { className: "check small", children: [_jsx("input", { type: "checkbox", checked: follow, onChange: (e) => setFollow(e.target.checked) }), " Follow"] }), _jsxs("a", { className: "btn btn-glass btn-sm", href: "/api/admin/logs/download", children: [_jsx(Icon, { name: "download", size: 16 }), " Download"] })] })] }), _jsxs("div", { className: "log-box", ref: box, onScroll: (e) => {
                    const el = e.currentTarget;
                    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
                    if (atBottom !== follow)
                        setFollow(atBottom);
                }, children: [shown.map((l) => (_jsx("div", { className: `log-line ${l.level}`, children: l.text }, l.seq))), shown.length === 0 && _jsxs("div", { className: "muted", children: ["No log lines", filter || liveOnly ? ' match the filter' : ' yet', "."] })] })] }));
}
