import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { Avatar } from '../components/Avatar';
import { Icon } from '../components/Icon';
import { useApi, useDocumentTitle } from '../hooks';
import { donePicking } from '../profilePick';
export default function WhoIsWatching() {
    useDocumentTitle("Who's watching?");
    const { signedIn } = useAuth();
    const navigate = useNavigate();
    const state = useLocation().state;
    const next = state?.from && state.from !== '/who' ? state.from : '/';
    const { data: members, error: loadError } = useApi('/api/household');
    const [asking, setAsking] = useState(null);
    const [pin, setPin] = useState('');
    const [error, setError] = useState('');
    // While opening, the account's chosen member (if any) is tried before showing the picker.
    const [deciding, setDeciding] = useState(true);
    useEffect(() => {
        // The household couldn't load (an API key, the server restarting): carry on as who this is.
        if (loadError && !members) {
            donePicking();
            navigate(next, { replace: true });
        }
    }, [loadError]); // eslint-disable-line react-hooks/exhaustive-deps
    useEffect(() => {
        if (!members)
            return;
        const leave = () => {
            donePicking();
            navigate(next, { replace: true });
        };
        // One person in the household: nothing to pick.
        if (members.length < 2)
            return leave();
        const auto = state?.opening ? members.find((m) => m.auto) : undefined;
        if (auto?.current)
            return leave();
        if (auto && !auto.hasPin) {
            api.post(`/api/household/${auto.id}/switch`, {}).then((res) => {
                signedIn(res.user);
                leave();
            }, () => setDeciding(false));
            return;
        }
        setDeciding(false);
    }, [members]); // eslint-disable-line react-hooks/exhaustive-deps
    const choose = async (m, code = '') => {
        if (m.hasPin && !m.current && !code) {
            setAsking(m);
            setPin('');
            setError('');
            return;
        }
        try {
            if (!m.current) {
                const res = await api.post(`/api/household/${m.id}/switch`, { pin: code });
                signedIn(res.user);
            }
            donePicking();
            navigate(next, { replace: true });
        }
        catch (e) {
            setError(e.message);
            setPin('');
        }
    };
    const submitPin = (e) => {
        e.preventDefault();
        if (asking)
            choose(asking, pin);
    };
    // Nothing to show until it's clear there is a choice to make.
    if (!members || deciding)
        return _jsx("div", { className: "auth-page" });
    return (_jsx("div", { className: "auth-page", children: _jsxs("div", { className: "who", children: [_jsx("h1", { children: asking ? `Enter ${asking.name}'s PIN` : "Who's watching?" }), asking ? (_jsxs("form", { className: "form who-pin", onSubmit: submitPin, children: [_jsx("input", { className: "passcode-input", type: "password", inputMode: "numeric", pattern: "[0-9]*", maxLength: 8, value: pin, onChange: (e) => setPin(e.target.value.replace(/\D/g, '')), autoFocus: true, "aria-label": "PIN" }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", children: [_jsx("button", { className: "btn btn-primary", disabled: pin.length < 4, children: "Continue" }), _jsx("button", { type: "button", className: "btn btn-glass", onClick: () => setAsking(null), children: "Back" })] })] })) : (_jsx("div", { className: "who-grid", children: members?.map((m) => (_jsxs("button", { className: `who-card ${m.current ? 'current' : ''}`, onClick: () => choose(m), children: [_jsx(Avatar, { name: m.name, src: m.avatar, className: "who-avatar" }), _jsx("span", { className: "who-name", children: m.name }), m.hasPin && !m.current && _jsx(Icon, { name: "lock", size: 14, className: "muted" })] }, m.id))) })), !asking && error && _jsx("div", { className: "form-error", children: error })] }) }));
}
