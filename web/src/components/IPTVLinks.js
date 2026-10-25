import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../api';
import { Icon } from './Icon';
/**
 * The signed-in user's M3U playlist and XMLTV guide URLs for IPTV apps, with
 * a button that makes an API key to put in them. The playlist lists only the
 * channels this user can watch.
 */
export function IPTVLinks({ onCreated }) {
    const [key, setKey] = useState(null);
    const origin = window.location.origin;
    const create = async () => {
        try {
            setKey((await api.post('/api/me/keys', { name: 'IPTV app' })).key);
            onCreated?.();
        }
        catch (err) {
            alert(err.message);
        }
    };
    const urls = [
        ['M3U playlist', `${origin}/api/live/playlist.m3u?token=${key ?? '…'}`],
        ['XMLTV guide', `${origin}/api/live/epg.xml?token=${key ?? '…'}`],
    ];
    return (_jsxs("div", { className: "panel facts", children: [urls.map(([label, url]) => (_jsxs("div", { style: { display: 'contents' }, children: [_jsx("dt", { children: label }), _jsxs("dd", { className: "mono", style: { wordBreak: 'break-all' }, children: [url, key && (_jsx("button", { type: "button", className: "btn btn-glass btn-sm", style: { marginLeft: 8 }, onClick: () => navigator.clipboard?.writeText(url), children: "Copy" }))] })] }, label))), _jsx("dd", { className: "muted small", style: { gridColumn: '1 / -1' }, children: key
                    ? "Copy these into TiviMate, VLC or another IPTV app now; the key won't be shown again. Delete it under API keys to cut the app off."
                    : 'Apps like TiviMate or VLC get the shared, fail-over streams. Create an API key for the app so it never needs your password.' }), !key && (_jsx("dd", { style: { gridColumn: '1 / -1' }, children: _jsxs("button", { type: "button", className: "btn btn-glass btn-sm", onClick: create, children: [_jsx(Icon, { name: "key", size: 16 }), " Create a key for an IPTV app"] }) }))] }));
}
