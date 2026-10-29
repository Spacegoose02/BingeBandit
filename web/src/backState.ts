import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useLocation, useNavigationType } from 'react-router-dom'

// Per history entry memory, so Back lands where you were: the page's scroll
// position, how much of an endless grid was loaded, a row's sideways scroll,
// a picked filter. Kept in sessionStorage so a reload keeps it too.
const STORE = 'bb.back'
const KEEP = 100 // history entries remembered

let cache: Record<string, Record<string, unknown>> | null = null
let timer = 0

function all() {
  if (!cache) {
    try {
      cache = JSON.parse(sessionStorage.getItem(STORE) ?? '{}') ?? {}
    } catch {
      cache = {}
    }
  }
  return cache!
}

function flush() {
  window.clearTimeout(timer)
  timer = 0
  try {
    sessionStorage.setItem(STORE, JSON.stringify(all()))
  } catch {
    // Private mode or full storage: Back still works within this tab's memory.
  }
}
if (typeof window !== 'undefined') window.addEventListener('pagehide', flush)

export function readBack<T>(key: string, name: string): T | undefined {
  return all()[key]?.[name] as T | undefined
}

export function writeBack(key: string, name: string, value: unknown) {
  const store = all()
  const entry = store[key] ?? {}
  // Re-inserted last so the oldest entries are the ones dropped.
  delete store[key]
  store[key] = { ...entry, [name]: value }
  const keys = Object.keys(store)
  for (const k of keys.slice(0, Math.max(0, keys.length - KEEP))) delete store[k]
  if (!timer) timer = window.setTimeout(flush, 300)
}

/** useState that Back restores: the value is remembered for this history entry. */
export function useBackState<T>(name: string, initial: T): [T, (v: T) => void] {
  const { key } = useLocation()
  const [value, setValue] = useState<T>(() => readBack<T>(key, name) ?? initial)
  const set = useCallback(
    (v: T) => {
      setValue(v)
      writeBack(key, name, v)
    },
    [key, name],
  )
  return [value, set]
}

/**
 * New pages open at the top; Back and Forward return to where the page was
 * scrolled. Pages load their content after they mount, so the position is
 * re-applied as the page grows (which also brings an endless grid's next
 * page in) until it can be reached, the page settles, or you scroll yourself.
 */
export function ScrollRestorer() {
  const loc = useLocation()
  const type = useNavigationType()
  const keyRef = useRef(loc.key)
  const restoring = useRef(false)
  const lastPath = useRef(loc.pathname)

  useEffect(() => {
    if ('scrollRestoration' in history) history.scrollRestoration = 'manual'
    const onScroll = () => {
      if (!restoring.current) writeBack(keyRef.current, 'scroll', Math.round(window.scrollY))
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  useLayoutEffect(() => {
    keyRef.current = loc.key
    const pathChanged = lastPath.current !== loc.pathname
    lastPath.current = loc.pathname
    if (type !== 'POP') {
      // A tab or search change replaces the entry in place and stays put.
      if (type === 'PUSH' || pathChanged) window.scrollTo(0, 0)
      return
    }
    const y = readBack<number>(loc.key, 'scroll') ?? 0
    if (y <= 0) {
      window.scrollTo(0, 0)
      return
    }

    restoring.current = true
    let stopped = false
    let frame = 0
    const started = performance.now()
    let settledAt = 0
    let height = 0
    const stop = () => {
      stopped = true
    }
    const inputs = ['wheel', 'touchstart', 'keydown', 'mousedown'] as const
    inputs.forEach((e) => window.addEventListener(e, stop, { passive: true }))
    const finish = () => {
      cancelAnimationFrame(frame)
      inputs.forEach((e) => window.removeEventListener(e, stop))
      restoring.current = false
    }
    const tick = () => {
      if (stopped) return finish()
      const doc = document.documentElement
      const max = doc.scrollHeight - window.innerHeight
      window.scrollTo(0, Math.min(y, max))
      const now = performance.now()
      if (doc.scrollHeight !== height) {
        height = doc.scrollHeight
        settledAt = now
      }
      // Reached, and nothing above it has moved for a moment (rows still
      // arriving would push it down), or the page never got that long.
      if ((max >= y && now - settledAt > 600) || now - started > 10000) return finish()
      frame = requestAnimationFrame(tick)
    }
    tick()
    return finish
  }, [loc.key]) // eslint-disable-line react-hooks/exhaustive-deps

  return null
}

/**
 * Keeps a sideways-scrolling element's position for Back. `ready` is true
 * once its content is drawn; the position is put back then.
 */
export function useBackScrollLeft(name: string, el: React.RefObject<HTMLElement>, ready: boolean) {
  const { key } = useLocation()
  const restored = useRef(false)
  useEffect(() => {
    const node = el.current
    if (!node) return
    if (!restored.current && ready) {
      restored.current = true
      const x = readBack<number>(key, name)
      if (x) node.scrollLeft = x
    }
    const onScroll = () => writeBack(key, name, Math.round(node.scrollLeft))
    node.addEventListener('scroll', onScroll, { passive: true })
    return () => node.removeEventListener('scroll', onScroll)
  }, [key, name, el, ready])
}
