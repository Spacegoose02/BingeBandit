import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { api, ApiError } from '../api';
import { useAuth } from '../auth';
import { Icon } from '../components/Icon';
import { passkeysSupported } from '../passkeys';
import { AuthFrame } from './Setup';
export default function Login() {
    const { login, loginWithPasskey, serverName } = useAuth();
    const nav = useNavigate();
    const loc = useLocation();
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [code, setCode] = useState('');
    const [needsCode, setNeedsCode] = useState(false);
    const [withCode, setWithCode] = useState(false);
    const [error, setError] = useState(() => new URLSearchParams(loc.search).get('sso_error') ?? '');
    const [busy, setBusy] = useState(false);
    const [sso, setSSO] = useState([]);
    useEffect(() => {
        api.get('/api/auth/oidc').then(setSSO, () => undefined);
    }, []);
    const next = loc.state?.from ?? '/';
    const submit = async (e) => {
        e.preventDefault();
        setBusy(true);
        setError('');
        try {
            await login(username, password, needsCode ? code : undefined);
            nav(next, { replace: true });
        }
        catch (err) {
            if (err instanceof ApiError && err.data?.needsCode) {
                // Only show the message once they've actually tried a code.
                if (needsCode)
                    setError(err.message);
                setNeedsCode(true);
                setCode('');
            }
            else {
                setError(err.message);
            }
        }
        finally {
            setBusy(false);
        }
    };
    const passkey = async () => {
        setBusy(true);
        setError('');
        try {
            if (await loginWithPasskey())
                nav(next, { replace: true });
        }
        catch (err) {
            setError(err.message);
        }
        finally {
            setBusy(false);
        }
    };
    if (withCode) {
        return (_jsx(AuthFrame, { subtitle: `Sign in to ${serverName}`, children: _jsx(LinkCode, { onCancel: () => setWithCode(false) }) }));
    }
    return (_jsx(AuthFrame, { subtitle: `Sign in to ${serverName}`, children: _jsxs("form", { onSubmit: submit, className: "form", children: [needsCode ? (_jsxs("label", { children: ["Code from your authenticator app", _jsx("input", { value: code, onChange: (e) => setCode(e.target.value), autoFocus: true, required: true, inputMode: "numeric", autoComplete: "one-time-code", placeholder: "123456" }), _jsx("span", { className: "muted small", children: "Lost your phone? Enter one of your recovery codes instead." })] })) : (_jsxs(_Fragment, { children: [_jsxs("label", { children: ["Username", _jsx("input", { value: username, onChange: (e) => setUsername(e.target.value), autoFocus: true, required: true, autoComplete: "username" })] }), _jsxs("label", { children: ["Password", _jsx("input", { type: "password", value: password, onChange: (e) => setPassword(e.target.value), required: true, autoComplete: "current-password" })] })] })), error && _jsx("div", { className: "form-error", children: error }), _jsx("button", { className: "btn btn-primary btn-block", disabled: busy, children: busy ? 'Signing in…' : needsCode ? 'Verify' : 'Sign in' }), needsCode ? (_jsx("button", { type: "button", className: "btn btn-glass btn-block", onClick: () => { setNeedsCode(false); setError(''); }, children: "Back" })) : (_jsxs(_Fragment, { children: [passkeysSupported() && (_jsxs("button", { type: "button", className: "btn btn-glass btn-block", onClick: passkey, disabled: busy, children: [_jsx(Icon, { name: "key", size: 18 }), " Sign in with a passkey"] })), _jsx("button", { type: "button", className: "btn btn-glass btn-block", onClick: () => setWithCode(true), children: "Sign in with a code instead" }), sso.length > 0 && _jsx("div", { className: "or-divider", children: "or" }), sso.map((p) => (_jsxs("a", { className: "btn btn-glass btn-block", href: `/api/auth/oidc/${p.id}/login?next=${encodeURIComponent(next)}`, children: ["Continue with ", p.name] }, p.id)))] }))] }) }));
}
/** Shows a code to enter on a signed-in phone or computer — for TVs, where typing a password is painful. */
function LinkCode({ onCancel }) {
    const { signedIn } = useAuth();
    const [link, setLink] = useState(null);
    const [error, setError] = useState('');
    const [attempt, setAttempt] = useState(0);
    useEffect(() => {
        let stopped = false;
        let timer = 0;
        setError('');
        setLink(null);
        api
            .post('/api/link/start', { device: 'Web (linked)' })
            .then((l) => {
            if (stopped)
                return;
            setLink(l);
            const poll = async () => {
                try {
                    const res = await api.get(`/api/link/poll?web=1&token=${l.pollToken}`);
                    if (stopped)
                        return;
                    if (res) {
                        signedIn(res.user);
                        return;
                    }
                    timer = window.setTimeout(poll, 2000);
                }
                catch (err) {
                    if (!stopped)
                        setError(err.message);
                }
            };
            timer = window.setTimeout(poll, 2000);
        })
            .catch((err) => !stopped && setError(err.message));
        return () => {
            stopped = true;
            clearTimeout(timer);
        };
    }, [attempt, signedIn]);
    return (_jsxs("div", { className: "form link-code", children: [_jsxs("p", { className: "muted", children: ["On a phone or computer that's already signed in, go to", _jsx("br", {}), _jsx("strong", { children: link ? link.linkUrl.replace(/\?code=.*$/, '') : '…' }), _jsx("br", {}), "and enter this code:"] }), _jsx("div", { className: "link-code-value", "aria-live": "polite", children: link ? `${link.code.slice(0, 3)}-${link.code.slice(3)}` : '······' }), error ? (_jsxs(_Fragment, { children: [_jsx("div", { className: "form-error", children: error }), _jsx("button", { type: "button", className: "btn btn-primary btn-block", onClick: () => setAttempt((a) => a + 1), children: "Get a new code" })] })) : (_jsx("p", { className: "muted small", children: "Waiting for you to approve it\u2026 The code works for 10 minutes." })), _jsx("button", { type: "button", className: "btn btn-glass btn-block", onClick: onCancel, children: "Use a password instead" })] }));
}
