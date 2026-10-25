import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useId, useState } from 'react';
import { Icon } from './Icon';
/** Editable list of short strings (genres, labels, collections). */
export function ChipsInput({ value, onChange, suggestions = [], placeholder, }) {
    const [text, setText] = useState('');
    const listId = useId();
    const add = (raw) => {
        const v = raw.trim();
        if (v && !value.some((x) => x.toLowerCase() === v.toLowerCase()))
            onChange([...value, v]);
        setText('');
    };
    return (_jsxs("div", { className: "chips-input", children: [value.map((v) => (_jsxs("span", { className: "chip removable", children: [v, _jsx("button", { type: "button", onClick: () => onChange(value.filter((x) => x !== v)), "aria-label": `Remove ${v}`, children: _jsx(Icon, { name: "x", size: 12 }) })] }, v))), _jsx("input", { list: listId, value: text, placeholder: value.length ? '' : placeholder, onChange: (e) => {
                    // Picking a datalist suggestion fires change with the full value.
                    const v = e.target.value;
                    if (v.endsWith(','))
                        add(v.slice(0, -1));
                    else if (suggestions.includes(v))
                        add(v);
                    else
                        setText(v);
                }, onKeyDown: (e) => {
                    if (e.key === 'Enter') {
                        e.preventDefault();
                        add(text);
                    }
                    else if (e.key === 'Backspace' && !text && value.length) {
                        onChange(value.slice(0, -1));
                    }
                }, onBlur: () => text && add(text) }), _jsx("datalist", { id: listId, children: suggestions
                    .filter((s) => !value.includes(s))
                    .map((s) => (_jsx("option", { value: s }, s))) })] }));
}
