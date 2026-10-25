import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api, img } from '../api';
import { useAuth } from '../auth';
import { PersonCard, PosterCard } from '../components/Cards';
import { AddToPlaylist, WatchlistButton } from '../components/AddToPlaylist';
import { EditItem } from '../components/EditItem';
import { MatchDialog } from '../components/MatchDialog';
import { MetaLine } from '../components/Hero';
import { RecommendButton } from '../components/Recommend';
import { Icon } from '../components/Icon';
import { Modal } from '../components/Modal';
import { Row } from '../components/Row';
import { useApi, useDocumentTitle } from '../hooks';
import { channelLabel, episodeLabel, fileSummary, formatBytes, formatClock, formatRuntime, languageName, progressFraction } from '../format';
export default function ItemPage() {
    const { id } = useParams();
    const { data, error, reload } = useApi(`/api/items/${id}`);
    useDocumentTitle(data?.item.title);
    if (error)
        return _jsx("div", { className: "page", children: _jsx("div", { className: "empty", children: error }) });
    if (!data)
        return _jsx("div", { className: "page-loading" });
    return _jsx(ItemView, { detail: data, reload: reload }, id);
}
function ItemView({ detail, reload }) {
    const { item, files = [], children = [], show, season, upNext, collections = [], canDownload } = detail;
    const { user } = useAuth();
    const nav = useNavigate();
    const [fileIdx, setFileIdx] = useState(0);
    const [trailer, setTrailer] = useState(false);
    const [busy, setBusy] = useState(false);
    const [dialog, setDialog] = useState(null);
    const file = files[fileIdx];
    const backdrop = img(item.backdrop || show?.backdrop || item.showBackdrop, 'original');
    const poster = img(item.kind === 'episode' ? season?.poster || show?.poster : item.poster || show?.poster, 'w500');
    const playable = item.kind === 'movie' || item.kind === 'episode';
    const resumeAt = item.watch.position > 15 ? Math.floor(item.watch.position) : 0;
    const fileQuery = file && files.length > 1 ? `file=${file.id}` : '';
    const play = (fromStart = false) => {
        const q = [fileQuery, !fromStart && resumeAt ? `t=${resumeAt}` : ''].filter(Boolean).join('&');
        nav(`/play/${item.id}${q ? `?${q}` : ''}`);
    };
    const toggleWatched = async () => {
        const fullyWatched = playable ? item.watch.watched : item.unwatchedCount === 0;
        await api.post(`/api/items/${item.id}/${fullyWatched ? 'unwatched' : 'watched'}`);
        reload();
    };
    const refresh = async () => {
        setBusy(true);
        try {
            await api.post(`/api/admin/items/${item.id}/refresh`);
            reload();
        }
        catch (e) {
            alert(e.message);
        }
        finally {
            setBusy(false);
        }
    };
    const detectMarkers = async () => {
        await api.post(`/api/admin/items/${item.id}/detect-markers`).catch((e) => alert(e.message));
        alert(item.kind === 'movie'
            ? 'Looking for the end credits in the background. It takes a few seconds; play the movie afterwards to see the Skip Credits button.'
            : 'Detecting intros and credits in the background. It takes a few seconds per episode; open an episode afterwards to see the results.');
    };
    const watchedNow = playable ? item.watch.watched : !!item.childCount && item.unwatchedCount === 0;
    const cast = (item.kind === 'season' || item.kind === 'episode' ? show?.credits : item.credits) ?? [];
    const directors = cast.filter((c) => c.kind === 'crew');
    return (_jsxs("div", { className: "detail", children: [backdrop && _jsx("div", { className: "detail-backdrop", style: { backgroundImage: `url(${backdrop})` } }), _jsx("div", { className: "detail-shade" }), _jsxs("div", { className: "detail-content", children: [_jsxs("div", { className: "detail-main", children: [_jsx("div", { className: `detail-poster ${item.kind === 'episode' ? 'is-episode' : ''}`, children: item.kind === 'episode' && img(item.backdrop, 'w780') ? (_jsx("img", { src: img(item.backdrop, 'w780'), alt: "" })) : poster ? (_jsx("img", { src: poster, alt: "" })) : (_jsx("div", { className: "poster-fallback", children: item.title })) }), _jsxs("div", { className: "detail-info", children: [(item.kind === 'season' || item.kind === 'episode') && show && (_jsxs("div", { className: "crumbs", children: [_jsx(Link, { to: `/item/${show.id}`, children: show.title }), item.kind === 'episode' && season && (_jsxs(_Fragment, { children: [_jsx(Icon, { name: "next", size: 14 }), _jsx(Link, { to: `/item/${season.id}`, children: season.title })] }))] })), item.logo && item.kind !== 'season' && item.kind !== 'episode' ? (_jsx("img", { className: "detail-logo", src: img(item.logo, 'w500'), alt: item.title })) : (_jsxs("h1", { className: "detail-title", children: [item.kind === 'episode' && _jsx("span", { className: "ep-label", children: episodeLabel(item) }), item.title] })), _jsx(MetaLine, { item: item }), detail.externalRatings && _jsx(ExternalScores, { r: detail.externalRatings }), item.tagline && _jsx("p", { className: "tagline", children: item.tagline }), _jsxs("div", { className: "actions", children: [playable && (_jsxs("button", { className: "btn btn-primary", onClick: () => play(), disabled: !file, children: [_jsx(Icon, { name: "play", fill: true, size: 18 }), resumeAt ? `Resume from ${formatClock(resumeAt)}` : 'Play'] })), playable && resumeAt > 0 && (_jsxs("button", { className: "btn btn-glass", onClick: () => play(true), children: [_jsx(Icon, { name: "replay10", size: 18 }), " From start"] })), item.kind === 'show' && upNext && (_jsxs(Link, { className: "btn btn-primary", to: `/play/${upNext.id}${upNext.watch.position > 15 ? `?t=${Math.floor(upNext.watch.position)}` : ''}`, children: [_jsx(Icon, { name: "play", fill: true, size: 18 }), upNext.watch.position > 15 ? 'Resume' : 'Play', " ", episodeLabel(upNext)] })), item.trailer && (_jsxs("button", { className: "btn btn-glass", onClick: () => setTrailer(true), children: [_jsx(Icon, { name: "film", size: 18 }), " Trailer"] })), _jsx("button", { className: "btn btn-glass icon-only", onClick: toggleWatched, title: watchedNow ? 'Mark unwatched' : 'Mark watched', children: _jsx(Icon, { name: "check", size: 18, className: watchedNow ? 'accent' : '' }) }), (item.kind === 'movie' || item.kind === 'show') && _jsx(WatchlistButton, { item: item, onChange: reload }), _jsx(AddToPlaylist, { item: item }), (item.kind === 'movie' || item.kind === 'show') && _jsx(RecommendButton, { item: item }), canDownload && file && (_jsx("a", { className: "btn btn-glass icon-only", href: `/api/files/${file.id}/direct?download=1`, download: true, title: "Download original file", children: _jsx(Icon, { name: "download", size: 18 }) })), user?.isAdmin && (_jsx("button", { className: "btn btn-glass icon-only", onClick: () => setDialog('edit'), title: "Edit details and artwork", children: _jsx(Icon, { name: "edit", size: 18 }) })), user?.isAdmin && (item.kind === 'movie' || item.kind === 'show') && (_jsx("button", { className: "btn btn-glass icon-only", onClick: () => setDialog('match'), title: "Fix match", children: _jsx(Icon, { name: "search", size: 18 }) })), user?.isAdmin && (item.kind === 'show' || item.kind === 'season' || item.kind === 'movie') && (_jsx("button", { className: "btn btn-glass icon-only", onClick: detectMarkers, title: "Detect intros and credits now", children: _jsx(Icon, { name: "activity", size: 18 }) })), user?.isAdmin && item.kind !== 'season' && (_jsx("button", { className: "btn btn-glass icon-only", onClick: refresh, disabled: busy, title: "Refresh metadata", children: _jsx(Icon, { name: "refresh", size: 18, className: busy ? 'spin' : '' }) }))] }), playable && progressFraction(item) > 0 && (_jsx("div", { className: "progress inline", children: _jsx("div", { style: { width: `${progressFraction(item) * 100}%` } }) })), item.overview && _jsx("p", { className: "overview", children: item.overview }), _jsxs("dl", { className: "facts", children: [directors.length > 0 && (_jsxs(_Fragment, { children: [_jsx("dt", { children: directors[0].role === 'Creator' ? 'Created by' : 'Directed by' }), _jsx("dd", { children: directors.filter((d) => d.role === directors[0].role).map((d) => d.name).join(', ') })] })), item.studio && (_jsxs(_Fragment, { children: [_jsx("dt", { children: item.kind === 'show' ? 'Network' : 'Studio' }), _jsx("dd", { children: item.studio })] })), item.releaseDate && (_jsxs(_Fragment, { children: [_jsx("dt", { children: item.kind === 'show' ? 'First aired' : 'Released' }), _jsx("dd", { children: new Date(item.releaseDate + 'T00:00:00').toLocaleDateString(undefined, { dateStyle: 'long' }) })] }))] }), file && _jsx(FileInfo, { files: files, index: fileIdx, onChange: setFileIdx, admin: !!user?.isAdmin })] })] }), item.kind === 'show' && children.length > 0 && (_jsx(Row, { title: "Seasons", children: children.map((s) => (_jsx(PosterCard, { item: s }, s.id))) })), item.kind === 'season' && _jsx(EpisodeList, { episodes: children }), cast.filter((c) => c.kind === 'cast').length > 0 && (_jsx(Row, { title: "Cast & Crew", children: cast.map((c, i) => (_jsx(PersonCard, { name: c.name, role: c.role, photo: c.photo }, `${c.name}-${i}`))) })), collections.map(({ collection, items }) => (_jsx(Row, { title: collection.name, action: _jsx(Link, { to: `/collection/${collection.id}`, className: "muted small", children: "View collection" }), children: items.map((m) => (_jsx(PosterCard, { item: m }, m.id))) }, collection.id)))] }), dialog === 'edit' && (_jsx(EditItem, { item: item, collectionNames: detail.collectionNames ?? [], markers: detail.markers, onClose: () => setDialog(null), onChanged: reload, onSaved: () => {
                    setDialog(null);
                    reload();
                } })), dialog === 'match' && (_jsx(MatchDialog, { item: item, tvdb: detail.tvdbId ? { id: detail.tvdbId, order: detail.tvdbOrder ?? '' } : undefined, onClose: () => setDialog(null), onMatched: () => {
                    setDialog(null);
                    reload();
                } })), trailer && item.trailer && (_jsx(Modal, { title: `${item.title} — Trailer`, onClose: () => setTrailer(false), wide: true, children: _jsx("div", { className: "video-embed", children: _jsx("iframe", { src: `https://www.youtube-nocookie.com/embed/${item.trailer}?autoplay=1&rel=0`, title: "Trailer", allow: "autoplay; encrypted-media; picture-in-picture; fullscreen", allowFullScreen: true }) }) }))] }));
}
function FileInfo({ files, index, onChange, admin }) {
    const f = files[index];
    const audio = f.streams.filter((s) => s.type === 'audio');
    const subs = f.streams.filter((s) => s.type === 'subtitle');
    return (_jsxs("div", { className: "file-info", children: [files.length > 1 && (_jsxs("label", { className: "select-label", children: ["Version", _jsx("select", { value: index, onChange: (e) => onChange(Number(e.target.value)), children: files.map((x, i) => (_jsxs("option", { value: i, children: [fileSummary(x), " \u00B7 ", formatBytes(x.size)] }, x.id))) })] })), _jsxs("div", { className: "chips", children: [_jsx("span", { className: "chip strong", children: fileSummary(f) }), f.bitDepth > 8 && _jsxs("span", { className: "chip", children: [f.bitDepth, "-bit"] }), _jsx("span", { className: "chip", children: f.container.toUpperCase() }), _jsx("span", { className: "chip", children: formatBytes(f.size) }), f.duration > 0 && _jsx("span", { className: "chip", children: formatRuntime(Math.round(f.duration / 60)) })] }), audio.length > 0 && (_jsxs("div", { className: "track-line", children: [_jsx("span", { className: "muted", children: "Audio" }), audio.map((a) => `${languageName(a.language)} (${a.codec.toUpperCase()} ${channelLabel(a.channels ?? 2)})`).join(' · ')] })), subs.length > 0 && (_jsxs("div", { className: "track-line", children: [_jsx("span", { className: "muted", children: "Subtitles" }), subs.map((s) => languageName(s.language) + (s.forced ? ' (forced)' : '')).join(' · ')] })), admin && _jsx("div", { className: "track-line path", title: f.path, children: f.path })] }));
}
function EpisodeList({ episodes }) {
    return (_jsxs("section", { className: "episodes", children: [_jsxs("h2", { children: [episodes.length, " Episodes"] }), episodes.map((ep) => {
                const still = img(ep.backdrop, 'w500');
                const pct = progressFraction(ep);
                return (_jsxs("div", { className: "episode", children: [_jsxs(Link, { to: `/play/${ep.id}${ep.watch.position > 15 ? `?t=${Math.floor(ep.watch.position)}` : ''}`, className: "episode-still", children: [still ? _jsx("img", { src: still, alt: "", loading: "lazy" }) : _jsx("div", { className: "poster-fallback", children: episodeLabel(ep) }), pct > 0 && (_jsx("div", { className: "progress", children: _jsx("div", { style: { width: `${pct * 100}%` } }) })), ep.watch.watched && _jsx("span", { className: "badge-watched", children: _jsx(Icon, { name: "check", size: 14 }) }), _jsx("div", { className: "poster-hover", children: _jsx(Icon, { name: "play", fill: true, size: 30 }) })] }), _jsxs("div", { className: "episode-body", children: [_jsxs(Link, { to: `/item/${ep.id}`, className: "episode-title", children: [_jsxs("span", { className: "ep-num", children: [ep.episodeNumber, "."] }), " ", ep.title] }), _jsx("div", { className: "muted small", children: [formatRuntime(ep.runtime), ep.releaseDate && new Date(ep.releaseDate + 'T00:00:00').toLocaleDateString()].filter(Boolean).join(' · ') }), ep.overview && _jsx("p", { className: "episode-overview", children: ep.overview })] })] }, ep.id));
            })] }));
}
/** IMDb, Rotten Tomatoes and Metacritic scores, each linking to the source when it can. */
function ExternalScores({ r }) {
    const imdb = r.imdbId ? `https://www.imdb.com/title/${r.imdbId}/` : undefined;
    return (_jsxs("div", { className: "external-scores", children: [!!r.imdb && (_jsxs("a", { className: "ext-score imdb", href: imdb, target: "_blank", rel: "noreferrer", title: r.imdbVotes ? `${r.imdbVotes.toLocaleString()} votes` : undefined, children: [_jsx("span", { className: "ext-badge", children: "IMDb" }), " ", r.imdb.toFixed(1)] })), !!r.rottenTomatoes && (_jsxs("span", { className: "ext-score", title: "Rotten Tomatoes critics", children: [_jsx("span", { className: "ext-badge rt", children: r.rottenTomatoes >= 60 ? '🍅' : '🤢' }), " ", r.rottenTomatoes, "%"] })), !!r.rottenTomatoesAudience && (_jsxs("span", { className: "ext-score", title: "Rotten Tomatoes audience", children: [_jsx("span", { className: "ext-badge rt", children: "\uD83C\uDF7F" }), " ", r.rottenTomatoesAudience, "%"] })), !!r.metacritic && (_jsxs("span", { className: "ext-score", title: "Metacritic", children: [_jsx("span", { className: `ext-badge mc ${r.metacritic >= 61 ? 'good' : r.metacritic >= 40 ? 'mixed' : 'bad'}`, children: r.metacritic }), " Metacritic"] }))] }));
}
