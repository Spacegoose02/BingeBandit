import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { Icon } from './Icon';
/** Where the room's playback is right now, in seconds (skew: server clock minus ours, ms). */
export function roomPosition(s, skew) {
    return s.paused ? s.position : s.position + Math.max(0, Date.now() + skew - s.at) / 1000;
}
export const togetherLink = (room) => `${window.location.origin}/play/${room.state.itemId}?together=${room.id}`;
/**
 * Keeps a player in a Watch Together room: applies everyone else's changes
 * as they arrive and returns send() for this viewer's own play, pause, seek
 * and next-item changes.
 */
export function useTogether(roomId, onRemote) {
    const [room, setRoom] = useState(null);
    const [ended, setEnded] = useState(false);
    const lastRev = useRef(0);
    const skew = useRef(0);
    const remote = useRef(onRemote);
    remote.current = onRemote;
    useEffect(() => {
        setRoom(null);
        setEnded(false);
        if (!roomId)
            return;
        lastRev.current = 0;
        const es = new EventSource(`/api/together/${roomId}/events`);
        es.onmessage = (e) => {
            const r = JSON.parse(e.data);
            skew.current = r.now - Date.now();
            setRoom(r);
            if (r.state.rev > lastRev.current) {
                const first = lastRev.current === 0;
                lastRev.current = r.state.rev;
                if (!first)
                    remote.current(r.state, skew.current, r);
            }
        };
        es.onerror = () => {
            // EventSource reconnects by itself; a room that is gone answers 404 and stops it.
            if (es.readyState === EventSource.CLOSED)
                setEnded(true);
        };
        return () => es.close();
    }, [roomId]);
    const send = useCallback((s) => {
        if (!roomId)
            return;
        api
            .post(`/api/together/${roomId}/state`, s)
            .then((r) => {
            lastRev.current = Math.max(lastRev.current, r.state.rev);
            setRoom(r);
        })
            .catch(() => undefined);
    }, [roomId]);
    /** The room's position now, by the server's clock. */
    const position = useCallback(() => (room ? roomPosition(room.state, skew.current) : 0), [room]);
    return { room, ended, send, position };
}
/** The Watch Together panel in the player: start a room, share it, invite, leave. */
export function TogetherPanel({ itemId, room, roomId, onStart, onLeave, }) {
    const { user } = useAuth();
    const [people, setPeople] = useState(null);
    const [picked, setPicked] = useState([]);
    const [msg, setMsg] = useState('');
    const [busy, setBusy] = useState(false);
    useEffect(() => {
        setPeople(null);
        if (!roomId)
            return;
        api.get(`/api/together/${roomId}/people`).then(setPeople, () => setPeople([]));
    }, [roomId, itemId]);
    if (!roomId) {
        return (_jsxs("div", { className: "menu player-menu together-panel", children: [_jsx("div", { className: "menu-section", children: "Watch together" }), _jsx("p", { className: "muted small", children: "Watch with someone else on this server, each on your own screen. Play, pause and skipping stay in step for everyone." }), _jsxs("button", { className: "btn btn-primary btn-sm", disabled: busy, onClick: async () => {
                        setBusy(true);
                        await onStart().catch((e) => setMsg(e.message));
                        setBusy(false);
                    }, children: [_jsx(Icon, { name: "users", size: 16 }), " Start a watch party"] }), msg && _jsx("div", { className: "form-error small", children: msg })] }));
    }
    const others = (people ?? []).filter((p) => !room?.members.some((m) => m.userId === p.id));
    const invite = async () => {
        setMsg('');
        try {
            await api.post(`/api/together/${roomId}/invite`, { userIds: picked });
            setMsg(picked.length === 1 ? 'Invitation sent.' : 'Invitations sent.');
            setPicked([]);
        }
        catch (e) {
            setMsg(e.message);
        }
    };
    return (_jsxs("div", { className: "menu player-menu together-panel", children: [_jsx("div", { className: "menu-section", children: "Watching together" }), _jsx("div", { className: "together-members", children: (room?.members ?? []).map((m) => (_jsxs("span", { className: "pill", children: [m.name, m.userId === user?.id && ' (you)'] }, m.userId))) }), room && (_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => {
                    navigator.clipboard?.writeText(togetherLink(room));
                    setMsg('Link copied. Anyone signed in to this server who can watch this can join with it.');
                }, children: [_jsx(Icon, { name: "link", size: 16 }), " Copy invite link"] })), others.length > 0 && (_jsxs(_Fragment, { children: [_jsx("div", { className: "menu-section", children: "Invite" }), others.map((p) => (_jsxs("label", { className: "check small", children: [_jsx("input", { type: "checkbox", checked: picked.includes(p.id), onChange: (e) => setPicked(e.target.checked ? [...picked, p.id] : picked.filter((x) => x !== p.id)) }), p.name] }, p.id))), _jsx("button", { className: "btn btn-primary btn-sm", disabled: !picked.length, onClick: invite, children: "Send invitation" })] })), people && others.length === 0 && (_jsx("p", { className: "muted small", children: "Only people who accept recommendations from others can be invited by name. Share the link with anyone else." })), msg && _jsx("div", { className: "muted small", children: msg }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: onLeave, children: "Leave the watch party" })] }));
}
/** Shows invitations to watch parties, checked every 20 seconds. */
export function TogetherInvites() {
    const [invites, setInvites] = useState([]);
    const nav = useNavigate();
    useEffect(() => {
        let stopped = false;
        const check = () => api.get('/api/together/invites').then((l) => !stopped && setInvites(l), () => undefined);
        check();
        const t = window.setInterval(() => document.visibilityState === 'visible' && check(), 20000);
        return () => {
            stopped = true;
            window.clearInterval(t);
        };
    }, []);
    if (!invites.length)
        return null;
    const decline = (i) => {
        setInvites((l) => l.filter((x) => x.room !== i.room));
        api.del(`/api/together/${i.room}/invite`).catch(() => undefined);
    };
    return (_jsx("div", { className: "together-invites", children: invites.map((i) => (_jsxs("div", { className: "notice together-invite", children: [_jsx(Icon, { name: "users", size: 18 }), _jsxs("span", { className: "grow", children: [_jsx("strong", { children: i.from }), " invited you to watch ", _jsx("strong", { children: i.title }), " together."] }), _jsx("button", { className: "btn btn-primary btn-sm", onClick: () => nav(`/play/${i.itemId}?together=${i.room}`), children: "Join" }), _jsx("button", { className: "btn btn-glass btn-sm", onClick: () => decline(i), children: "Not now" })] }, i.room))) }));
}
