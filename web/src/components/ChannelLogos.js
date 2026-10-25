import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useRef, useState } from 'react';
import { api, ApiError } from '../api';
import { useApi } from '../hooks';
import { Icon } from './Icon';
/** A channel's logo field value for a library logo. */
export const libraryRef = (l) => `library:${l.file}`;
/** Where to show a logo field value from: library logos are served by the server, URLs as they are. */
export const logoSrc = (value) => (value.startsWith('library:') ? `/api/live/logos/${value.slice(8)}` : value);
/** Adds images to the logo library; with match, each also goes to channels whose names match its file name. */
export async function uploadLogos(files, match) {
    const body = new FormData();
    for (const f of Array.from(files))
        body.append('file', f);
    if (match)
        body.append('match', '1');
    const res = await fetch('/api/admin/live/logos', {
        method: 'POST',
        body,
        credentials: 'same-origin',
        headers: { 'X-Requested-With': 'BingeBandit' },
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok)
        throw new ApiError(res.status, data.error ?? res.statusText);
    return data;
}
export const LOGO_TYPES = 'image/png,image/jpeg,image/svg+xml,image/webp,image/gif';
/** The logo input in channel editors: a URL, or a pick from the logo library (with upload). */
export function LogoField({ value, onChange }) {
    const [open, setOpen] = useState(false);
    const { data, setData } = useApi(open ? '/api/admin/live/logos' : null);
    const [q, setQ] = useState('');
    const [error, setError] = useState('');
    const fileRef = useRef(null);
    const isLibrary = value.startsWith('library:');
    const upload = async (files) => {
        setError('');
        try {
            const res = await uploadLogos(files, false);
            if (res.failed?.length)
                setError(res.failed.join('; '));
            const before = new Set((data ?? []).map((l) => l.id));
            const list = await api.get('/api/admin/live/logos');
            setData(list);
            const added = list.find((l) => !before.has(l.id));
            if (added && files.length === 1)
                onChange(libraryRef(added));
        }
        catch (e) {
            setError(e.message);
        }
    };
    const shown = (data ?? []).filter((l) => !q || l.name.toLowerCase().includes(q.toLowerCase()));
    return (_jsxs("div", { className: "logo-field", children: [_jsxs("div", { className: "url-row", children: [_jsx("div", { className: "art-thumb logo logo-preview", children: value ? _jsx("img", { src: logoSrc(value), alt: "" }) : _jsx("span", { className: "muted small", children: "None" }) }), isLibrary ? (_jsx("span", { className: "chip strong grow", children: "From the logo library" })) : (_jsx("input", { value: value, onChange: (e) => onChange(e.target.value), placeholder: "Logo URL" })), _jsxs("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setOpen(!open), children: [_jsx(Icon, { name: "image", size: 16 }), " Library"] }), value && (_jsx("button", { type: "button", className: "icon-btn", onClick: () => onChange(''), "aria-label": "Remove logo", children: _jsx(Icon, { name: "x", size: 16 }) }))] }), open && (_jsxs("div", { className: "logo-picker", children: [_jsxs("div", { className: "actions", children: [_jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Find a logo", style: { width: 200 } }), _jsxs("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => fileRef.current?.click(), children: [_jsx(Icon, { name: "upload", size: 16 }), " Upload"] }), _jsx("input", { ref: fileRef, type: "file", accept: LOGO_TYPES, multiple: true, hidden: true, onChange: (e) => e.target.files?.length && upload(e.target.files) })] }), error && _jsx("div", { className: "form-error small", children: error }), _jsx("div", { className: "art-grid logo compact", children: shown.map((l) => (_jsx("button", { type: "button", className: `art-thumb logo ${value === libraryRef(l) ? 'chosen' : ''}`, title: l.name, onClick: () => {
                                onChange(libraryRef(l));
                                setOpen(false);
                            }, children: _jsx("img", { src: l.url, alt: l.name, loading: "lazy" }) }, l.id))) }), data?.length === 0 && _jsx("div", { className: "muted small", children: "The logo library is empty. Upload images here or under Live TV \u2192 Logos." })] }))] }));
}
