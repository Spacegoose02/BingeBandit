import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useState } from 'react';
import { Outlet } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { Icon } from './Icon';
import { Modal } from './Modal';
export const LOCKED_EVENT = 'bb:locked';
const UNLOCKED_EVENT = 'bb:unlocked';
// Gates on screen take care of a lapsed unlock themselves, so the pop-up
// prompt stays away while one is mounted.
let gates = 0;
function PasscodeForm({ onUnlocked, hasPassword }) {
    const [usePassword, setUsePassword] = useState(false);
    const [value, setValue] = useState('');
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const submit = async (e) => {
        e.preventDefault();
        setBusy(true);
        setError('');
        try {
            await api.post('/api/me/passcode/unlock', usePassword ? { password: value } : { passcode: value });
            window.dispatchEvent(new Event(UNLOCKED_EVENT));
            onUnlocked();
        }
        catch (err) {
            setError(err.message);
            setValue('');
        }
        setBusy(false);
    };
    return (_jsxs("form", { className: "form", onSubmit: submit, children: [_jsxs("label", { children: [usePassword ? 'Account password' : 'Settings passcode', usePassword ? (_jsx("input", { type: "password", value: value, onChange: (e) => setValue(e.target.value), autoFocus: true, required: true, autoComplete: "current-password" })) : (_jsx("input", { className: "passcode-input", type: "password", inputMode: "numeric", pattern: "[0-9]*", maxLength: 8, value: value, onChange: (e) => setValue(e.target.value.replace(/\D/g, '')), autoFocus: true, required: true, autoComplete: "off" }))] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-primary", disabled: busy || !value, children: [_jsx(Icon, { name: "unlock", size: 16 }), " Unlock"] }), hasPassword && (_jsx("button", { type: "button", className: "btn btn-glass", onClick: () => { setUsePassword(!usePassword); setValue(''); setError(''); }, children: usePassword ? 'Use the passcode' : 'Forgot it? Use your password' }))] })] }));
}
/** Route wrapper for Account and Admin: asks for the passcode before showing them. */
export function PasscodeGate({ children }) {
    const { user } = useAuth();
    const [status, setStatus] = useState(null);
    const check = useCallback(() => api.get('/api/me/passcode').then(setStatus).catch(() => setStatus({ set: false, unlocked: true })), []);
    useEffect(() => {
        gates++;
        check();
        const locked = () => setStatus((s) => (s ? { ...s, unlocked: false } : s));
        window.addEventListener(LOCKED_EVENT, locked);
        window.addEventListener(UNLOCKED_EVENT, check);
        return () => {
            gates--;
            window.removeEventListener(LOCKED_EVENT, locked);
            window.removeEventListener(UNLOCKED_EVENT, check);
        };
    }, [check, user?.hasPasscode]);
    if (!status)
        return null;
    if (status.set && !status.unlocked) {
        return (_jsx("div", { className: "page narrow", children: _jsxs("div", { className: "panel passcode-panel", children: [_jsx(Icon, { name: "lock", size: 28 }), _jsx("h2", { children: "Settings are locked" }), _jsx("p", { className: "muted", children: "Enter your settings passcode to change your account or the server." }), _jsx(PasscodeForm, { onUnlocked: check, hasPassword: !!user?.hasPassword })] }) }));
    }
    return _jsx(_Fragment, { children: children ?? _jsx(Outlet, {}) });
}
/** Pop-up prompt for a locked action outside the gated pages (e.g. editing an item). */
export function PasscodePrompt() {
    const { user } = useAuth();
    const [open, setOpen] = useState(false);
    useEffect(() => {
        const show = () => gates === 0 && setOpen(true);
        window.addEventListener(LOCKED_EVENT, show);
        return () => window.removeEventListener(LOCKED_EVENT, show);
    }, []);
    if (!open)
        return null;
    return (_jsxs(Modal, { title: "Settings are locked", onClose: () => setOpen(false), children: [_jsx("p", { className: "muted", children: "Enter your settings passcode, then try again." }), _jsx(PasscodeForm, { onUnlocked: () => setOpen(false), hasPassword: !!user?.hasPassword })] }));
}
/** Account section: set, change or remove the passcode, or lock now. */
export function PasscodeSettings() {
    const { user, refresh } = useAuth();
    const [value, setValue] = useState('');
    const [editing, setEditing] = useState(false);
    const [msg, setMsg] = useState(null);
    if (!user)
        return null;
    const save = async (e) => {
        e.preventDefault();
        try {
            await api.put('/api/me/passcode', { passcode: value });
            setMsg({ ok: true, text: user.hasPasscode ? 'Passcode changed.' : 'Passcode set. Other browsers and devices now need it to change settings.' });
            setValue('');
            setEditing(false);
            refresh();
        }
        catch (err) {
            setMsg({ ok: false, text: err.message });
        }
    };
    const remove = async () => {
        if (!confirm('Remove the settings passcode? Anyone signed in as you can then change settings.'))
            return;
        await api.del('/api/me/passcode').catch((e) => alert(e.message));
        setMsg({ ok: true, text: 'Passcode removed.' });
        refresh();
    };
    const lockNow = async () => {
        await api.post('/api/me/passcode/lock').catch(() => { });
        window.dispatchEvent(new Event(LOCKED_EVENT));
    };
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Settings passcode" }), _jsxs("p", { className: "muted small", children: ["Lock your account settings", user.isAdmin && ' and the server admin pages', " behind a 4 to 8 digit passcode, so whoever uses this account can watch but not change anything. Once entered, settings stay unlocked for 15 minutes of use. Apps and API keys aren't affected."] }), editing || !user.hasPasscode ? (_jsxs("form", { onSubmit: save, className: "field-row", children: [_jsx("input", { className: "passcode-input", type: "password", inputMode: "numeric", pattern: "[0-9]{4,8}", minLength: 4, maxLength: 8, placeholder: user.hasPasscode ? 'New passcode' : 'Choose a passcode', value: value, onChange: (e) => setValue(e.target.value.replace(/\D/g, '')), autoComplete: "off", required: true }), _jsx("button", { className: "btn btn-primary", children: user.hasPasscode ? 'Change' : 'Set passcode' }), editing && (_jsx("button", { type: "button", className: "btn btn-glass", onClick: () => setEditing(false), children: "Cancel" }))] })) : (_jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-glass", onClick: lockNow, children: [_jsx(Icon, { name: "lock", size: 16 }), " Lock now"] }), _jsx("button", { className: "btn btn-glass", onClick: () => setEditing(true), children: "Change passcode" }), _jsx("button", { className: "btn btn-glass danger", onClick: remove, children: "Remove" })] })), msg && _jsx("div", { className: msg.ok ? 'form-ok' : 'form-error', children: msg.text })] }));
}
