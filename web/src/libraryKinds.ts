import type { IconName } from './components/Icon'
import type { Item, LibraryKind } from './types'

/** How each kind of library is named and drawn across the web app. */
export const libraryKinds: Record<LibraryKind, { label: string; icon: IconName; one: string; many: string; tip: string }> = {
  movies: { label: 'Movies', icon: 'film', one: 'movie', many: 'movies', tip: 'Tip: name folders "Movie Title (Year)" for the best matches.' },
  shows: { label: 'TV Shows', icon: 'tv', one: 'show', many: 'shows', tip: 'Tip: use "Show Name/Season 01/Show Name - S01E01.mkv" for the best matches.' },
  music: {
    label: 'Music',
    icon: 'music',
    one: 'album',
    many: 'albums',
    tip: 'Albums are read from each song\'s tags (artist, album, track number). Untagged songs work too as "Artist/Album/01 - Song.mp3". A cover.jpg in the album folder becomes its artwork.',
  },
  audiobooks: {
    label: 'Audiobooks',
    icon: 'headphones',
    one: 'audiobook',
    many: 'audiobooks',
    tip: 'Each folder is a book ("Author/Book Title/01.mp3"), or one .m4b file is a whole book. Tags win over folder names.',
  },
  photos: {
    label: 'Photos',
    icon: 'image',
    one: 'album',
    many: 'albums',
    tip: 'Each folder of pictures becomes an album. JPEG, PNG, GIF, WebP and AVIF are shown; HEIC and RAW files are skipped.',
  },
}

export const isVideoLibrary = (k: LibraryKind) => k === 'movies' || k === 'shows'

/** Square artwork: album covers, audiobook covers and photos. */
export const squareArt = (it: Item) => ['artist', 'album', 'track', 'audiobook', 'chapter', 'photoalbum', 'photo'].includes(it.kind)

/** m:ss or h:mm:ss. */
export function formatTime(s: number): string {
  if (!isFinite(s) || s < 0) s = 0
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.floor(s % 60)
  const mm = h ? String(m).padStart(2, '0') : String(m)
  return `${h ? `${h}:` : ''}${mm}:${String(sec).padStart(2, '0')}`
}

/** The line under a music, audiobook or photo card. */
export function mediaSubtitle(it: Item): string {
  switch (it.kind) {
    case 'album':
      return [it.studio, it.year].filter(Boolean).join(' · ')
    case 'audiobook':
      return it.studio || (it.year ? String(it.year) : '')
    case 'artist':
      return 'Artist'
    case 'photoalbum':
      return it.year ? String(it.year) : ''
    case 'track':
    case 'chapter':
      return [it.studio, it.showTitle].filter(Boolean).join(' · ')
  }
  return it.year ? String(it.year) : ''
}
