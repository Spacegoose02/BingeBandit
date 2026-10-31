import { desktop } from './desktop'

// TV mode: the web app driven by a remote or the arrow keys, for the Windows
// app on a living-room PC. Arrows move focus to the nearest item in that
// direction, OK/Enter clicks it, Back/Escape goes back, and the menu key
// opens a card's menu. On by default in the Windows app; in a browser,
// ?tv=1 turns it on (and ?tv=0 off) for this browser.

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
// Reached another way: menu buttons on cards and screens (the menu key), the second
// link on a landscape card (the picture already goes there); rows scroll
// by themselves as focus moves along them.
const SKIP = '.card-menu-btn, .wide-card a.card-title, .row-arrow, [data-menu]'

type Dir = 'up' | 'down' | 'left' | 'right'
const DIRS: Record<string, Dir> = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right' }

let last: HTMLElement | null = null

export function tvModeOn(): boolean {
  const p = new URLSearchParams(location.search).get('tv')
  try {
    if (p === '1') localStorage.setItem('bb.tv', '1')
    if (p === '0') localStorage.removeItem('bb.tv')
    return !!desktop || localStorage.getItem('bb.tv') === '1'
  } catch {
    return !!desktop || p === '1'
  }
}

/** Where focus may go now: inside an open menu or dialog when there is one. */
function scope(): ParentNode {
  const menus = document.querySelectorAll<HTMLElement>('[role="menu"]')
  const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]')
  return menus[menus.length - 1] ?? dialogs[dialogs.length - 1] ?? document
}

function visible(el: HTMLElement): boolean {
  const r = el.getBoundingClientRect()
  if (r.width < 2 || r.height < 2) return false
  const s = getComputedStyle(el)
  return s.visibility !== 'hidden' && s.display !== 'none' && Number(s.opacity) > 0.05
}

function candidates(): HTMLElement[] {
  return [...scope().querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => !el.matches(SKIP) && !el.closest('[inert], [aria-hidden="true"]') && visible(el))
}

/**
 * The best element from `from` in `dir`: it must lie beyond `from`'s centre
 * that way; distance along the direction counts, and being off to the side
 * counts three times as much, so rows and columns are followed.
 */
export function nearest(from: DOMRect, dir: Dir, rects: { el: HTMLElement; r: DOMRect }[]): HTMLElement | null {
  const cx = from.left + from.width / 2
  const cy = from.top + from.height / 2
  let best: HTMLElement | null = null
  let bestScore = Infinity
  for (const { el, r } of rects) {
    const x = r.left + r.width / 2
    const y = r.top + r.height / 2
    let along: number
    let side: number
    const overlapX = Math.max(0, Math.min(from.right, r.right) - Math.max(from.left, r.left))
    const overlapY = Math.max(0, Math.min(from.bottom, r.bottom) - Math.max(from.top, r.top))
    if (dir === 'right' || dir === 'left') {
      if (dir === 'right' ? x <= cx + 1 || r.left < from.left + 1 : x >= cx - 1 || r.right > from.right - 1) continue
      along = dir === 'right' ? Math.max(0, r.left - from.right) : Math.max(0, from.left - r.right)
      side = overlapY > 0 ? Math.abs(y - cy) * 0.1 : Math.abs(y - cy)
    } else {
      if (dir === 'down' ? y <= cy + 1 || r.top < from.top + 1 : y >= cy - 1 || r.bottom > from.bottom - 1) continue
      along = dir === 'down' ? Math.max(0, r.top - from.bottom) : Math.max(0, from.top - r.bottom)
      side = overlapX > 0 ? Math.abs(x - cx) * 0.1 : Math.abs(x - cx)
    }
    const score = along + side * 3
    if (score < bestScore) {
      bestScore = score
      best = el
    }
  }
  return best
}

/** The part of the screen an element is in: the sidebar, the page, or a dialog. */
const region = (el: Element) => el.closest('[role="menu"], [role="dialog"], aside, main')

function focus(el: HTMLElement) {
  last = el
  el.focus({ preventScroll: true })
  // Menus close when anything scrolls, and their items are always in view.
  if (el.closest('[role="menu"]')) return
  // Rows scroll sideways to it ('nearest'); the page scrolls only when it's
  // near the top or bottom edge, then brings it to the middle.
  const r = el.getBoundingClientRect()
  const margin = 80
  const offScreen = r.top < margin || r.bottom > innerHeight - margin
  el.scrollIntoView({ block: offScreen ? (r.height > innerHeight - 2 * margin ? 'start' : 'center') : 'nearest', inline: 'nearest', behavior: 'smooth' })
}

/** Something sensible to start from: where focus last was, else the page's first item. */
function start(): HTMLElement | null {
  const list = candidates()
  if (last && last.isConnected && list.includes(last)) return last
  // On a page: where it says to start, its main button (Play), else its first card, else its first item.
  const main = list.filter((el) => el.closest('main'))
  return (
    list.find((el) => el.matches('[data-tv-start]')) ??
    main.find((el) => el.matches('.btn-primary')) ??
    main.find((el) => el.matches('.poster-link, .wide-frame, .episode-still')) ??
    (main.length ? main : list)[0] ??
    null
  )
}

const typing = (el: Element | null) =>
  el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement || (el instanceof HTMLInputElement && !['checkbox', 'radio', 'button', 'submit', 'range'].includes(el.type))

function onKey(e: KeyboardEvent) {
  if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return
  // The browser players have their own keys.
  if (document.querySelector('.player')) return
  const active = document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null
  const dir = DIRS[e.key]
  if (dir) {
    // Left and right move the cursor in a text box; up and down leave it.
    if (typing(active) && (dir === 'left' || dir === 'right' || active instanceof HTMLSelectElement)) return
    e.preventDefault()
    // A card's menu button (focused again when its menu closes) counts as its card.
    const origin = active?.matches(SKIP) ? active.closest('.has-menu')?.querySelector<HTMLElement>('.poster-link, a[href]') ?? active : active
    if (!origin || !candidates().includes(origin)) {
      const s = start()
      if (s) focus(s)
      return
    }
    // Up and down stay in the sidebar or the page. Left and right stay in
    // the row, and at its end only cross between the sidebar and the page.
    const here = region(origin)
    const from = origin.getBoundingClientRect()
    let rects = candidates()
      .filter((el) => el !== origin)
      .map((el) => ({ el, r: el.getBoundingClientRect() }))
    if (dir === 'up' || dir === 'down') {
      rects = rects.filter(({ el }) => region(el) === here)
    } else {
      const sameRow = ({ r }: { r: DOMRect }) => Math.min(from.bottom, r.bottom) - Math.max(from.top, r.top) > 0
      const ahead = (d: { r: DOMRect }) => (dir === 'right' ? d.r.left >= from.left + 1 : d.r.right <= from.right - 1)
      const row = rects.filter((d) => region(d.el) === here && sameRow(d) && ahead(d))
      rects = row.length ? row : rects.filter(({ el }) => region(el) !== here)
    }
    const next = nearest(from, dir, rects)
    if (next) focus(next)
    return
  }
  if (e.key === 'Escape' || e.key === 'Backspace' || e.key === 'BrowserBack' || e.key === 'GoBack') {
    if (e.key === 'Backspace' && typing(active)) return
    // Open menus and dialogs close themselves on Escape.
    if (document.querySelector('[role="menu"], [role="dialog"]')) return
    if (location.pathname === '/') return
    e.preventDefault()
    history.back()
    return
  }
  if (e.key === 'ContextMenu' || e.key === 'm') {
    if (e.key === 'm' && typing(active)) return
    const btn = active?.closest('.has-menu')?.querySelector<HTMLButtonElement>('.card-menu-btn:not(.card-edit-btn), [data-menu]')
    if (btn) {
      e.preventDefault()
      btn.click()
      // Into the menu that just opened.
      requestAnimationFrame(() => {
        const first = document.querySelector<HTMLElement>('[role="menu"] [role^="menuitem"], [role="menu"] button')
        if (first) first.focus()
      })
    }
  }
}

/**
 * A page change leaves nothing focused; put focus on the new page once it
 * has drawn. Going back returns to the item that was focused there.
 */
let pageTimer: number | undefined
let pageKey = ''
const remembered = new Map<string, number>()
const historyKey = () => String((history.state as { key?: string } | null)?.key ?? location.pathname)

function leavePage() {
  if (last?.isConnected) remembered.set(pageKey, candidates().indexOf(last))
}

function onPage() {
  last = null
  pageKey = historyKey()
  const want = remembered.get(pageKey) ?? -1
  window.clearTimeout(pageTimer)
  let tries = 0
  const settle = () => {
    const a = document.activeElement
    if (a && a !== document.body && want < 0) return
    const list = candidates()
    // Rows load after the page; wait a little for the remembered one.
    if (want >= 0 && list.length <= want && ++tries < 10) {
      pageTimer = window.setTimeout(settle, 200)
      return
    }
    const s = list[want] ?? start()
    if (s) focus(s)
    else if (++tries < 10) pageTimer = window.setTimeout(settle, 200)
  }
  pageTimer = window.setTimeout(settle, 250)
}

export function enableTvMode() {
  document.documentElement.classList.add('tv')
  window.addEventListener('keydown', onKey)
  // react-router moves with pushState/replaceState; Back is popstate.
  for (const m of ['pushState', 'replaceState'] as const) {
    const orig = history[m].bind(history)
    history[m] = (...args: Parameters<History['pushState']>) => {
      const before = location.pathname
      if (m === 'pushState') leavePage()
      orig(...args)
      if (location.pathname !== before) onPage()
    }
  }
  // On Back the URL has changed but the old page is still drawn: remember it under its own key first.
  window.addEventListener('popstate', () => {
    leavePage()
    onPage()
  })
  onPage()
}
