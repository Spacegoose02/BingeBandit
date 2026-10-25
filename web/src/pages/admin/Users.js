import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { api } from '../../api';
import { useAuth } from '../../auth';
import { Avatar } from '../../components/Avatar';
import { Icon } from '../../components/Icon';
import { Modal } from '../../components/Modal';
import { useApi, useDocumentTitle } from '../../hooks';
export default function AdminUsers() {
    useDocumentTitle('Users');
    const { user: me } = useAuth();
    const { data: users, reload } = useApi('/api/admin/users');
    const [adding, setAdding] = useState(false);
    const [resetting, setResetting] = useState(null);
    const [access, setAccess] = useState(null);
    const remove = async (u) => {
        if (!confirm(`Delete ${u.username}? Their watch history is removed too.`))
            return;
        await api.del(`/api/admin/users/${u.id}`).catch((e) => alert(e.message));
        reload();
    };
    const clearPasscode = async (u) => {
        if (!confirm(`Remove ${u.username}'s settings passcode? They can set a new one under Account.`))
            return;
        await api.del(`/api/admin/users/${u.id}/passcode`).catch((e) => alert(e.message));
        reload();
    };
    const removePasskeys = async (u) => {
        if (!confirm(`Remove all of ${u.username}'s passkeys? They sign in with their password until they add new ones.`))
            return;
        await api.del(`/api/admin/users/${u.id}/passkeys`).catch((e) => alert(e.message));
        reload();
    };
    const setLiveTV = async (u, on) => {
        if (!on && !confirm(`Turn off Live TV for ${u.username}${u.ownerId ? '' : ' and their profiles'}? Anything they're watching stops, and Live TV and Recordings disappear for them.`))
            return;
        await api.put(`/api/admin/users/${u.id}/live-tv`, { on }).catch((e) => alert(e.message));
        reload();
    };
    const reset2FA = async (u) => {
        if (!confirm(`Turn off two-step sign-in for ${u.username}? They can sign in with just their password until they set it up again.`))
            return;
        await api.del(`/api/admin/users/${u.id}/2fa`).catch((e) => alert(e.message));
        reload();
    };
    return (_jsxs("div", { className: "page", children: [_jsxs("header", { className: "page-head", children: [_jsx("h1", { children: "Users" }), _jsxs("button", { className: "btn btn-primary", onClick: () => setAdding(true), children: [_jsx(Icon, { name: "plus", size: 18 }), " Add user"] })] }), _jsx("div", { className: "panel list", children: users?.map((u) => (_jsxs("div", { className: "list-row", children: [_jsx(Avatar, { name: u.username, src: u.avatar }), _jsxs("div", { className: "grow", children: [_jsxs("div", { children: [u.username, " ", u.id === me?.id && _jsx("span", { className: "muted small", children: "(you)" }), ' ', u.ownerId ? (_jsxs("span", { className: "pill", title: "A profile under another account; it has no password of its own", children: ["Profile of ", users?.find((o) => o.id === u.ownerId)?.username ?? 'an account'] })) : (!u.hasPassword && _jsx("span", { className: "pill", title: "Made by single sign-on; no password yet", children: "SSO only" }))] }), _jsxs("div", { className: "muted small", children: ["Joined ", new Date(u.createdAt * 1000).toLocaleDateString()] })] }), u.isAdmin ? (_jsx("span", { className: "pill ok", children: "Admin \u00B7 all libraries" })) : (_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setAccess(u), children: [_jsx(Icon, { name: "library", size: 16 }), " ", u.allLibraries ? 'All libraries' : `${u.libraryIds?.length ?? 0} libraries`] })), !u.isAdmin && (_jsxs("button", { className: `btn btn-glass btn-sm${u.liveTV ? '' : ' danger'}`, onClick: () => setLiveTV(u, !u.liveTV), title: u.liveTV ? 'Turn off Live TV and recordings for this user' : 'Let this user watch Live TV again', children: [_jsx(Icon, { name: "broadcast", size: 16 }), " ", u.liveTV ? 'Live TV on' : 'Live TV off'] })), u.hasPasscode && (_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => clearPasscode(u), title: "For someone who forgot their settings passcode", children: [_jsx(Icon, { name: "unlock", size: 16 }), " Clear passcode"] })), u.totpEnabled && (_jsx("button", { className: "btn btn-glass btn-sm", onClick: () => reset2FA(u), title: "For someone who lost their phone and recovery codes", children: "Turn off 2-step" })), !!u.passkeys && (_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => removePasskeys(u), title: "For a lost or shared device", children: ["Remove ", u.passkeys === 1 ? 'passkey' : `${u.passkeys} passkeys`] })), !u.ownerId && (_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setResetting(u), children: [_jsx(Icon, { name: "key", size: 16 }), " Reset password"] })), u.id !== me?.id && (_jsx("button", { className: "btn btn-glass btn-sm danger", onClick: () => remove(u), "aria-label": `Delete ${u.username}`, children: _jsx(Icon, { name: "trash", size: 16 }) }))] }, u.id))) }), adding && _jsx(AddUser, { onClose: () => setAdding(false), onSaved: () => { setAdding(false); reload(); } }), resetting && _jsx(ResetPassword, { user: resetting, onClose: () => setResetting(null) }), access && _jsx(LibraryAccess, { user: access, onClose: () => setAccess(null), onSaved: () => { setAccess(null); reload(); } })] }));
}
function LibraryAccess({ user, onClose, onSaved }) {
    const { data: libraries } = useApi('/api/admin/libraries');
    const [all, setAll] = useState(user.allLibraries);
    const [ids, setIds] = useState(new Set(user.libraryIds ?? []));
    const [error, setError] = useState('');
    const toggle = (id) => setIds((s) => {
        const n = new Set(s);
        if (n.has(id))
            n.delete(id);
        else
            n.add(id);
        return n;
    });
    const save = async (e) => {
        e.preventDefault();
        try {
            await api.put(`/api/admin/users/${user.id}/libraries`, { all, libraryIds: [...ids] });
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: `Libraries for ${user.username}`, onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: all, onChange: (e) => setAll(e.target.checked) }), "All libraries, including ones added later"] }), _jsx("div", { className: `check-list ${all ? 'disabled' : ''}`, children: libraries?.map((l) => (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", disabled: all, checked: all || ids.has(l.id), onChange: () => toggle(l.id) }), _jsx(Icon, { name: l.kind === 'movies' ? 'film' : 'tv', size: 16 }), " ", l.name] }, l.id))) }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: "Save" })] })] }) }));
}
function AddUser({ onClose, onSaved }) {
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [isAdmin, setIsAdmin] = useState(false);
    const [error, setError] = useState('');
    const submit = async (e) => {
        e.preventDefault();
        try {
            await api.post('/api/admin/users', { username, password, isAdmin });
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: "Add user", onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: submit, children: [_jsxs("label", { children: ["Username", _jsx("input", { value: username, onChange: (e) => setUsername(e.target.value), required: true, minLength: 2, autoFocus: true })] }), _jsxs("label", { children: ["Password", _jsx("input", { type: "password", value: password, onChange: (e) => setPassword(e.target.value), required: true, minLength: 8, autoComplete: "new-password" })] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: isAdmin, onChange: (e) => setIsAdmin(e.target.checked) }), "Admin (can manage libraries, users and settings)"] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: "Add user" })] })] }) }));
}
function ResetPassword({ user, onClose }) {
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const submit = async (e) => {
        e.preventDefault();
        try {
            await api.put(`/api/admin/users/${user.id}/password`, { password });
            onClose();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsx(Modal, { title: `Reset password for ${user.username}`, onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: submit, children: [_jsxs("label", { children: ["New password", _jsx("input", { type: "password", value: password, onChange: (e) => setPassword(e.target.value), required: true, minLength: 8, autoFocus: true, autoComplete: "new-password" })] }), _jsx("p", { className: "muted small", children: "They will be signed out on all devices." }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions end", children: [_jsx("button", { type: "button", className: "btn btn-glass", onClick: onClose, children: "Cancel" }), _jsx("button", { className: "btn btn-primary", children: "Reset password" })] })] }) }));
}
