import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useApi } from '../hooks';
import { Icon } from './Icon';
import { Modal } from './Modal';
/** Button and dialog for recommending a movie or show to other people. */
export function RecommendButton({ item }) {
    const [open, setOpen] = useState(false);
    const [done, setDone] = useState('');
    return (_jsxs("div", { className: "menu-anchor", children: [_jsx("button", { className: "btn btn-glass icon-only", onClick: () => setOpen(true), title: "Recommend to someone", children: _jsx(Icon, { name: "users", size: 18 }) }), done && _jsx("div", { className: "inline-note", children: done }), open && (_jsx(RecommendDialog, { item: item, onClose: (msg) => {
                    setOpen(false);
                    if (msg) {
                        setDone(msg);
                        window.setTimeout(() => setDone(''), 2500);
                    }
                } }))] }));
}
function RecommendDialog({ item, onClose }) {
    const { data: people } = useApi(`/api/items/${item.id}/recommend`);
    const [picked, setPicked] = useState([]);
    const [note, setNote] = useState('');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const toggle = (id) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
    const send = async () => {
        setBusy(true);
        setError('');
        try {
            const res = await api.post(`/api/items/${item.id}/recommend`, { userIds: picked, note });
            onClose(`Recommended to ${res.sent} ${res.sent === 1 ? 'person' : 'people'}`);
        }
        catch (e) {
            setError(e.message);
            setBusy(false);
        }
    };
    return (_jsx(Modal, { title: `Recommend ${item.title}`, onClose: () => onClose(), children: _jsxs("div", { className: "form", children: [people && people.length === 0 && (_jsxs("p", { className: "muted", children: ["Nobody can get recommendations yet. People turn them on under ", _jsx(Link, { to: "/account", children: "Account \u2192 Sharing" }), ", and they only see titles from libraries they can watch."] })), people && people.length > 0 && (_jsxs(_Fragment, { children: [_jsx("div", { className: "check-list", children: people.map((p) => (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: picked.includes(p.id), onChange: () => toggle(p.id) }), p.name] }, p.id))) }), _jsxs("label", { children: ["Note (optional)", _jsx("textarea", { rows: 2, maxLength: 280, value: note, onChange: (e) => setNote(e.target.value), placeholder: "Why they'll like it" })] })] })), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", children: [_jsx("button", { className: "btn btn-primary", disabled: busy || picked.length === 0, onClick: send, children: "Recommend" }), _jsx("button", { className: "btn btn-glass", onClick: () => onClose(), children: "Cancel" })] })] }) }));
}
