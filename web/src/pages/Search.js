import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { PosterCard } from '../components/Cards';
import { Icon } from '../components/Icon';
import { useDocumentTitle } from '../hooks';
export default function Search() {
    useDocumentTitle('Search');
    const [params, setParams] = useSearchParams();
    const [q, setQ] = useState(params.get('q') ?? '');
    const [results, setResults] = useState(null);
    useEffect(() => {
        const term = q.trim();
        const t = window.setTimeout(async () => {
            setParams(term ? { q: term } : {}, { replace: true });
            setResults(term ? await api.get(`/api/search?q=${encodeURIComponent(term)}`) : null);
        }, 250);
        return () => window.clearTimeout(t);
    }, [q, setParams]);
    return (_jsxs("div", { className: "page", children: [_jsxs("div", { className: "search-box", children: [_jsx(Icon, { name: "search", size: 22 }), _jsx("input", { value: q, onChange: (e) => setQ(e.target.value), placeholder: "Search movies and shows", autoFocus: true })] }), results && results.length === 0 && _jsxs("div", { className: "empty", children: ["No matches for \u201C", q, "\u201D."] }), _jsx("div", { className: "poster-grid", children: results?.map((it) => (_jsx(PosterCard, { item: it }, it.id))) })] }));
}
