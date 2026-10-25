import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { api, img } from '../api';
import { useAuth } from '../auth';
import { PosterCard } from '../components/Cards';
import { CollectionEditor } from '../components/CollectionEditor';
import { Icon } from '../components/Icon';
import { useApi, useDocumentTitle } from '../hooks';
import { smartRuleText } from '../format';
export default function CollectionPage() {
    const { id } = useParams();
    const { user } = useAuth();
    const nav = useNavigate();
    const { data, error, reload } = useApi(`/api/collections/${id}`);
    const [editing, setEditing] = useState(false);
    useDocumentTitle(data?.collection.name);
    if (error)
        return _jsx("div", { className: "page", children: _jsx("div", { className: "empty", children: error }) });
    if (!data)
        return _jsx("div", { className: "page-loading" });
    const { collection, items } = data;
    const backdrop = img(collection.backdrop || items.find((i) => i.backdrop)?.backdrop, 'original');
    const remove = async () => {
        if (!confirm(`Delete the collection "${collection.name}"? The movies and shows stay in your library.`))
            return;
        await api.del(`/api/admin/collections/${collection.id}`);
        nav(`/library/${collection.libraryId}?tab=collections`, { replace: true });
    };
    return (_jsxs("div", { className: "detail", children: [backdrop && _jsx("div", { className: "detail-backdrop short", style: { backgroundImage: `url(${backdrop})` } }), _jsx("div", { className: "detail-shade short" }), _jsx("div", { className: "detail-content", children: _jsxs("div", { className: "page", style: { paddingTop: 0 }, children: [_jsxs("header", { className: "page-head", children: [_jsxs("div", { className: "collection-head", children: [_jsxs("div", { className: "muted small", children: [_jsx(Icon, { name: "stack", size: 14 }), " Collection", smartRuleText(collection) ? ` · smart: ${smartRuleText(collection)}` : ''] }), _jsx("h1", { className: "detail-title", children: collection.name }), _jsxs("div", { className: "muted", children: [items.length, " ", items.length === 1 ? 'item' : 'items'] }), collection.overview && _jsx("p", { className: "overview", children: collection.overview })] }), user?.isAdmin && (_jsxs("div", { className: "actions", children: [_jsxs("button", { className: "btn btn-glass btn-sm", onClick: () => setEditing(true), children: [_jsx(Icon, { name: "edit", size: 16 }), " Edit"] }), _jsx("button", { className: "btn btn-glass btn-sm danger", onClick: remove, "aria-label": "Delete collection", children: _jsx(Icon, { name: "trash", size: 16 }) })] }))] }), items.length === 0 && _jsx("div", { className: "empty", children: "Nothing in this collection yet." }), _jsx("div", { className: "poster-grid", children: items.map((it) => (_jsx(PosterCard, { item: it }, it.id))) })] }) }), editing && (_jsx(CollectionEditor, { libraryId: collection.libraryId, collection: collection, onClose: () => setEditing(false), onSaved: () => {
                    setEditing(false);
                    reload();
                } }))] }));
}
