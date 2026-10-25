import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { useApi, useDocumentTitle } from '../hooks';
/** /link — approve a code shown on a TV (or another browser) to sign it in. */
export default function LinkDevice() {
    useDocumentTitle('Link a device');
    const { user } = useAuth();
    const [params] = useSearchParams();
    const [code, setCode] = useState(params.get('code') ?? '');
    const [userId, setUserId] = useState(0);
    const [msg, setMsg] = useState(null);
    const [busy, setBusy] = useState(false);
    const { data: users } = useApi(user?.isAdmin ? '/api/admin/users' : null);
    const submit = async (e) => {
        e.preventDefault();
        setBusy(true);
        setMsg(null);
        try {
            const res = await api.post('/api/link/approve', { code, userId });
            const who = users?.find((u) => u.id === userId)?.username;
            setMsg({ ok: true, text: `Done. The ${res.device} is now signed in${who && userId !== user?.id ? ` as ${who}` : ''}.` });
            setCode('');
        }
        catch (err) {
            setMsg({ ok: false, text: err.message });
        }
        finally {
            setBusy(false);
        }
    };
    return (_jsxs("div", { className: "page narrow", children: [_jsx("h1", { children: "Link a device" }), _jsx("p", { className: "muted", children: "Enter the code shown on your TV or other device to sign it in without typing a password." }), _jsxs("form", { className: "panel form", onSubmit: submit, children: [_jsxs("label", { children: ["Code", _jsx("input", { className: "link-code-input", value: code, onChange: (e) => setCode(e.target.value.toUpperCase()), placeholder: "ABC-123", autoFocus: true, required: true, autoComplete: "off", spellCheck: false })] }), user?.isAdmin && users && users.length > 1 && (_jsxs("label", { children: ["Sign it in as", _jsx("select", { value: userId || user.id, onChange: (e) => setUserId(Number(e.target.value)), children: users.map((u) => (_jsxs("option", { value: u.id, children: [u.username, u.id === user.id ? ' (you)' : ''] }, u.id))) })] })), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text }), _jsx("button", { className: "btn btn-primary", disabled: busy, children: busy ? 'Linking…' : 'Link device' })] })] }));
}
