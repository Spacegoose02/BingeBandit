import { jsx as _jsx } from "react/jsx-runtime";
import { createContext, useCallback, useContext, useEffect, useState } from 'react';
import { api } from './api';
import { getPasskey, passkeyErrorMessage } from './passkeys';
import { askForProfile } from './profilePick';
const AuthContext = createContext(null);
export function AuthProvider({ children }) {
    const [loading, setLoading] = useState(true);
    const [user, setUser] = useState(null);
    const [needsSetup, setNeedsSetup] = useState(false);
    const [serverName, setServerName] = useState('BingeBandit');
    const refresh = useCallback(async () => {
        try {
            const s = await api.get('/api/setup');
            setNeedsSetup(s.needsSetup);
            setServerName(s.serverName || 'BingeBandit');
            setUser(s.needsSetup ? null : await api.get('/api/me').catch(() => null));
        }
        finally {
            setLoading(false);
        }
    }, []);
    useEffect(() => {
        refresh();
        const onUnauthorized = () => setUser(null);
        window.addEventListener('bb:unauthorized', onUnauthorized);
        return () => window.removeEventListener('bb:unauthorized', onUnauthorized);
    }, [refresh]);
    const login = async (username, password, code) => {
        const res = await api.post('/api/auth/login', { username, password, code, device: 'Web' });
        askForProfile();
        setUser(res.user);
    };
    /** Signs in with a passkey; false when the person cancelled the browser's prompt. */
    const loginWithPasskey = async () => {
        const { flow, options } = await api.post('/api/auth/passkey/begin');
        let credential;
        try {
            credential = await getPasskey(options);
        }
        catch (err) {
            const msg = passkeyErrorMessage(err);
            if (!msg)
                return false;
            throw new Error(msg);
        }
        const res = await api.post('/api/auth/passkey/finish', { flow, credential, device: 'Web' });
        askForProfile();
        setUser(res.user);
        return true;
    };
    const setup = async (username, password) => {
        const res = await api.post('/api/setup', { username, password, device: 'Web' });
        setNeedsSetup(false);
        setUser(res.user);
    };
    const logout = async () => {
        await api.post('/api/auth/logout').catch(() => undefined);
        setUser(null);
    };
    return (_jsx(AuthContext.Provider, { value: { loading, user, needsSetup, serverName, refresh, login, loginWithPasskey, signedIn: setUser, setup, logout }, children: children }));
}
export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx)
        throw new Error('useAuth outside AuthProvider');
    return ctx;
}
