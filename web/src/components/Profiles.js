import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useAuth } from '../auth';
import { useApi } from '../hooks';
import { Avatar, AvatarPicker } from './Avatar';
import { Icon } from './Icon';
import { Modal } from './Modal';
/** Account section: the profiles under this account. */
export function Profiles() {
    const { user, refresh } = useAuth();
    const { data: members, reload } = useApi('/api/household');
    // A picture change can be the signed-in member's own, shown in the menu too.
    const changed = () => {
        reload();
        refresh();
    };
    const [name, setName] = useState('');
    const [editing, setEditing] = useState(null);
    const [error, setError] = useState('');
    if (!user)
        return null;
    if (user.ownerId) {
        return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Profile" }), _jsxs("p", { className: "muted small", children: ["This is a profile of another account. ", _jsx(Link, { to: "/who", children: "Switch profile" }), " to manage profiles from the account."] }), _jsx(AvatarPicker, { memberId: user.id, name: user.profileName ?? user.username, src: user.avatar, onChange: changed })] }));
    }
    const profiles = members?.filter((m) => !m.isOwner) ?? [];
    const account = members?.find((m) => m.isOwner);
    const auto = members?.find((m) => m.auto);
    const setAuto = async (memberId) => {
        setError('');
        await api.put('/api/household/autoselect', { memberId }).then(reload, (e) => setError(e.message));
    };
    const add = async (e) => {
        e.preventDefault();
        setError('');
        try {
            await api.post('/api/household', { name });
            setName('');
            reload();
        }
        catch (err) {
            setError(err.message);
        }
    };
    return (_jsxs("div", { className: "panel form", style: { marginBottom: 20 }, children: [_jsx("h3", { children: "Profiles" }), _jsxs("p", { className: "muted small", children: ["Give each person in your home their own profile: separate Continue Watching, watch history, watchlist and playback settings, all under this sign-in. BingeBandit asks who's watching when it opens, and you can switch from your name in the menu. A PIN keeps a profile private; the PIN for this main profile is your ", _jsx("strong", { children: "settings passcode" }), " below."] }), account && _jsx(AvatarPicker, { memberId: account.id, name: account.name, src: account.avatar, onChange: changed }), profiles.length > 0 && (_jsx("div", { className: "list", children: profiles.map((p) => (_jsxs("div", { className: "list-row", children: [_jsx(Avatar, { name: p.name, src: p.avatar }), _jsxs("div", { className: "grow", children: [_jsxs("div", { children: [p.name, " ", p.hasPin && _jsx(Icon, { name: "lock", size: 13, className: "muted" })] }), _jsx("div", { className: "muted small", children: p.allLibraries ? 'All your libraries' : `${p.libraryIds?.length ?? 0} libraries` })] }), _jsxs("button", { type: "button", className: "btn btn-glass btn-sm", onClick: () => setEditing(p), children: [_jsx(Icon, { name: "edit", size: 16 }), " Edit"] })] }, p.id))) })), _jsxs("form", { onSubmit: add, className: "field-row", children: [_jsx("input", { value: name, onChange: (e) => setName(e.target.value), placeholder: "Name, e.g. Kids", maxLength: 30, required: true }), _jsxs("button", { className: "btn btn-primary", children: [_jsx(Icon, { name: "plus", size: 16 }), " Add profile"] })] }), profiles.length > 0 && members && (_jsxs("label", { children: ["When BingeBandit opens", _jsxs("select", { value: auto?.id ?? 0, onChange: (e) => setAuto(Number(e.target.value)), children: [_jsx("option", { value: 0, children: "Ask who's watching" }), members.map((m) => (_jsxs("option", { value: m.id, children: ["Start as ", m.name, m.hasPin ? ' (asks for the PIN)' : ''] }, m.id)))] }), _jsx("span", { className: "muted small", children: "On every device signed in to this account. Switch profile from your name in the menu." })] })), error && _jsx("div", { className: "form-error", children: error }), editing && _jsx(EditProfile, { profile: editing, onClose: () => setEditing(null), onSaved: () => { setEditing(null); reload(); }, onPicture: reload })] }));
}
function EditProfile({ profile, onClose, onSaved, onPicture }) {
    const [avatar, setAvatar] = useState(profile.avatar);
    const { data: libraries } = useApi('/api/libraries');
    const [name, setName] = useState(profile.name);
    const [all, setAll] = useState(profile.allLibraries);
    const [ids, setIds] = useState(new Set(profile.libraryIds ?? []));
    const [pin, setPin] = useState('');
    const [error, setError] = useState('');
    const save = async (e) => {
        e.preventDefault();
        setError('');
        try {
            await api.put(`/api/household/${profile.id}`, { name, allLibraries: all, libraryIds: [...ids] });
            if (pin)
                await api.put(`/api/household/${profile.id}/pin`, { pin });
            onSaved();
        }
        catch (err) {
            setError(err.message);
        }
    };
    const removePin = async () => {
        await api.put(`/api/household/${profile.id}/pin`, { pin: '' }).catch((e) => setError(e.message));
        onSaved();
    };
    const remove = async () => {
        if (!confirm(`Delete the profile "${profile.name}"? Its watch history, watchlist and playlists are deleted too.`))
            return;
        await api.del(`/api/household/${profile.id}`).catch((e) => setError(e.message));
        onSaved();
    };
    const toggle = (id) => {
        const next = new Set(ids);
        if (next.has(id))
            next.delete(id);
        else
            next.add(id);
        setIds(next);
    };
    return (_jsx(Modal, { title: `Edit ${profile.name}`, onClose: onClose, children: _jsxs("form", { className: "form", onSubmit: save, children: [_jsx(AvatarPicker, { memberId: profile.id, name: name || profile.name, src: avatar, onChange: (src) => {
                        setAvatar(src);
                        onPicture();
                    } }), _jsxs("label", { children: ["Name", _jsx("input", { value: name, onChange: (e) => setName(e.target.value), maxLength: 30, required: true })] }), _jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: all, onChange: (e) => setAll(e.target.checked) }), "All my libraries, including ones added later"] }), !all && (_jsx("div", { className: "check-list", children: libraries?.map((l) => (_jsxs("label", { className: "check", children: [_jsx("input", { type: "checkbox", checked: ids.has(l.id), onChange: () => toggle(l.id) }), l.name] }, l.id))) })), _jsxs("label", { children: [profile.hasPin ? 'New PIN (leave empty to keep it)' : 'PIN (optional, 4 to 8 digits)', _jsx("input", { className: "passcode-input", type: "password", inputMode: "numeric", pattern: "[0-9]{4,8}", maxLength: 8, value: pin, onChange: (e) => setPin(e.target.value.replace(/\D/g, '')), autoComplete: "off" })] }), error && _jsx("div", { className: "form-error", children: error }), _jsxs("div", { className: "actions", children: [_jsx("button", { className: "btn btn-primary", children: "Save" }), profile.hasPin && (_jsx("button", { type: "button", className: "btn btn-glass", onClick: removePin, children: "Remove PIN" })), _jsxs("button", { type: "button", className: "btn btn-glass danger", onClick: remove, children: [_jsx(Icon, { name: "trash", size: 16 }), " Delete profile"] })] })] }) }));
}
