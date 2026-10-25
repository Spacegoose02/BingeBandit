import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { uploadBackup } from '../api';
import { useAuth } from '../auth';
export function AuthFrame({ children, subtitle }) {
    return (_jsx("div", { className: "auth-page", children: _jsxs("div", { className: "auth-card", children: [_jsx("img", { src: "/brand/stacked-light.png", alt: "BingeBandit", className: "auth-logo" }), _jsx("p", { className: "auth-sub", children: subtitle }), children] }) }));
}
export default function Setup() {
    const { setup, refresh } = useAuth();
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [confirm, setConfirm] = useState('');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const [restored, setRestored] = useState(false);
    const submit = async (e) => {
        e.preventDefault();
        if (password !== confirm)
            return setError('Passwords do not match');
        setBusy(true);
        setError('');
        try {
            await setup(username, password);
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    const restore = async (file) => {
        if (!window.confirm(`Set up this server from ${file.name}? Everyone signs in with the accounts from that server.`))
            return;
        setBusy(true);
        setError('');
        try {
            await uploadBackup('/api/setup/restore', file);
            setRestored(true);
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    if (restored) {
        return (_jsx(AuthFrame, { subtitle: "Restored from your backup.", children: _jsxs("div", { className: "form", children: [_jsx("p", { className: "muted", children: "Sign in with an admin account from your old server. If your media is in different folders here, fix the library folders in Settings, then restart the server so folder watching and Live TV pick up the restored settings." }), _jsx("button", { className: "btn btn-primary btn-block", onClick: () => refresh(), children: "Continue to sign in" })] }) }));
    }
    return (_jsx(AuthFrame, { subtitle: "Welcome! Create the admin account for this server.", children: _jsxs("form", { onSubmit: submit, className: "form", children: [_jsxs("label", { children: ["Username", _jsx("input", { value: username, onChange: (e) => setUsername(e.target.value), autoFocus: true, required: true, minLength: 2, autoComplete: "username" })] }), _jsxs("label", { children: ["Password", _jsx("input", { type: "password", value: password, onChange: (e) => setPassword(e.target.value), required: true, minLength: 8, autoComplete: "new-password" })] }), _jsxs("label", { children: ["Confirm password", _jsx("input", { type: "password", value: confirm, onChange: (e) => setConfirm(e.target.value), required: true, minLength: 8, autoComplete: "new-password" })] }), error && _jsx("div", { className: "form-error", children: error }), _jsx("button", { className: "btn btn-primary btn-block", disabled: busy, children: busy ? 'Creating…' : 'Create admin account' }), _jsxs("label", { className: `btn btn-glass btn-block${busy ? ' disabled' : ''}`, children: ["Moving from another server? Restore a backup", _jsx("input", { type: "file", accept: ".db,application/vnd.sqlite3,application/x-sqlite3", hidden: true, disabled: busy, onChange: (e) => {
                                const f = e.target.files?.[0];
                                e.target.value = '';
                                if (f)
                                    restore(f);
                            } })] })] }) }));
}
