export class ApiError extends Error {
    constructor(status, message, data) {
        super(message);
        this.status = status;
        this.data = data;
    }
}
async function request(method, path, body, init) {
    const headers = { 'X-Requested-With': 'BingeBandit' };
    // A FormData body (a file upload) sets its own multipart Content-Type.
    const form = body instanceof FormData;
    if (body !== undefined && !form)
        headers['Content-Type'] = 'application/json';
    const res = await fetch(path, {
        method,
        credentials: 'same-origin',
        headers,
        body: form ? body : body !== undefined ? JSON.stringify(body) : undefined,
        ...init,
    });
    if (res.status === 204 || res.status === 202)
        return undefined;
    const data = await res.json().catch(() => null);
    if (!res.ok) {
        if (res.status === 401 && !path.startsWith('/api/auth'))
            window.dispatchEvent(new Event('bb:unauthorized'));
        if (res.status === 423)
            window.dispatchEvent(new Event('bb:locked'));
        throw new ApiError(res.status, data?.error ?? res.statusText, data ?? undefined);
    }
    return data;
}
export const api = {
    get: (path) => request('GET', path),
    post: (path, body, init) => request('POST', path, body ?? {}, init),
    put: (path, body) => request('PUT', path, body ?? {}),
    patch: (path, body) => request('PATCH', path, body ?? {}),
    del: (path, init) => request('DELETE', path, undefined, init),
};
/** Sized artwork URL; TMDB widths: w92 w185 w342 w500 w780 w1280 original. */
export function img(url, width) {
    if (!url)
        return undefined;
    return url.startsWith('/api/images/') ? `${url}?w=${width}` : url;
}
/** Sends a database backup file as the request body (it can be large, so not as a form). */
export async function uploadBackup(path, file) {
    const res = await fetch(path, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'X-Requested-With': 'BingeBandit', 'Content-Type': 'application/octet-stream' },
        body: file,
    });
    const data = await res.json().catch(() => null);
    if (!res.ok) {
        if (res.status === 423)
            window.dispatchEvent(new Event('bb:locked'));
        throw new ApiError(res.status, data?.error ?? res.statusText, data ?? undefined);
    }
    return data;
}
