export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public data?: Record<string, unknown>,
  ) {
    super(message)
  }
}

async function request<T>(method: string, path: string, body?: unknown, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { 'X-Requested-With': 'BingeBandit' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    ...init,
  })
  if (res.status === 204 || res.status === 202) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    if (res.status === 401 && !path.startsWith('/api/auth')) window.dispatchEvent(new Event('bb:unauthorized'))
    if (res.status === 423) window.dispatchEvent(new Event('bb:locked'))
    throw new ApiError(res.status, data?.error ?? res.statusText, data ?? undefined)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown, init?: RequestInit) => request<T>('POST', path, body ?? {}, init),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  del: <T>(path: string, init?: RequestInit) => request<T>('DELETE', path, undefined, init),
}

/** Sized artwork URL; TMDB widths: w92 w185 w342 w500 w780 w1280 original. */
export function img(url: string | undefined, width: string): string | undefined {
  if (!url) return undefined
  return url.startsWith('/api/images/') ? `${url}?w=${width}` : url
}

/** Sends a database backup file as the request body (it can be large, so not as a form). */
export async function uploadBackup(path: string, file: File): Promise<{ restored: string; previous?: string }> {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'X-Requested-With': 'BingeBandit', 'Content-Type': 'application/octet-stream' },
    body: file,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    if (res.status === 423) window.dispatchEvent(new Event('bb:locked'))
    throw new ApiError(res.status, data?.error ?? res.statusText, data ?? undefined)
  }
  return data
}
