import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useRef, useState } from 'react';
import { api } from '../api';
import { Icon } from './Icon';
/** A profile picture, or the name's first letter when there is none. */
export function Avatar({ name, src, className = '' }) {
    const [broken, setBroken] = useState(false);
    if (src && !broken)
        return _jsx("img", { className: `avatar avatar-img ${className}`, src: src, alt: "", onError: () => setBroken(true) });
    return _jsx("span", { className: `avatar ${className}`, children: name.slice(0, 1).toUpperCase() });
}
const AVATAR_SIZE = 512;
/** Crops a picture to its centre square and shrinks it, so uploads stay small. */
async function squareJPEG(file) {
    const bitmap = await createImageBitmap(file).catch(() => {
        throw new Error('That file is not a picture this browser can open');
    });
    const side = Math.min(bitmap.width, bitmap.height);
    const out = Math.min(side, AVATAR_SIZE);
    const canvas = document.createElement('canvas');
    canvas.width = out;
    canvas.height = out;
    canvas.getContext('2d').drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, out, out);
    bitmap.close();
    return new Promise((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('Could not read the picture'))), 'image/jpeg', 0.9));
}
/** Upload or remove a household member's picture. */
export function AvatarPicker({ memberId, name, src, onChange, }) {
    const input = useRef(null);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const upload = async (file) => {
        setBusy(true);
        setError('');
        try {
            const body = new FormData();
            body.append('file', await squareJPEG(file), 'avatar.jpg');
            const res = await api.put(`/api/household/${memberId}/avatar`, body);
            onChange(res.avatar);
        }
        catch (e) {
            setError(e.message);
        }
        finally {
            setBusy(false);
        }
    };
    const remove = async () => {
        setError('');
        await api.del(`/api/household/${memberId}/avatar`).then(() => onChange(undefined), (e) => setError(e.message));
    };
    return (_jsxs("div", { className: "avatar-picker", children: [_jsx(Avatar, { name: name, src: src, className: "big" }), _jsxs("div", { className: "actions", children: [_jsxs("button", { type: "button", className: "btn btn-glass btn-sm", disabled: busy, onClick: () => input.current?.click(), children: [_jsx(Icon, { name: "image", size: 16 }), " ", busy ? 'Uploading…' : src ? 'Change picture' : 'Upload picture'] }), src && (_jsx("button", { type: "button", className: "btn btn-glass btn-sm", onClick: remove, children: "Remove" }))] }), _jsx("input", { ref: input, type: "file", accept: "image/*", hidden: true, onChange: (e) => {
                    const f = e.target.files?.[0];
                    e.target.value = '';
                    if (f)
                        upload(f);
                } }), error && _jsx("div", { className: "form-error", children: error })] }));
}
