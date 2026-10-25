import { useEffect, useRef, useState } from 'react'
import type { PreRoll as PreRollData } from '../types'

interface YTPlayer {
  destroy(): void
}
declare global {
  interface Window {
    YT?: { Player: new (el: HTMLElement, opts: object) => YTPlayer }
    onYouTubeIframeAPIReady?: () => void
  }
}

let ytReady: Promise<boolean> | undefined

/** Loads YouTube's iframe API once; false when it can't be reached. */
function loadYouTube(): Promise<boolean> {
  if (window.YT?.Player) return Promise.resolve(true)
  ytReady ??= new Promise((resolve) => {
    const timer = setTimeout(() => resolve(false), 8000)
    const prev = window.onYouTubeIframeAPIReady
    window.onYouTubeIframeAPIReady = () => {
      prev?.()
      clearTimeout(timer)
      resolve(true)
    }
    const s = document.createElement('script')
    s.src = 'https://www.youtube.com/iframe_api'
    s.onerror = () => {
      clearTimeout(timer)
      ytReady = undefined
      resolve(false)
    }
    document.head.appendChild(s)
  })
  return ytReady
}

type Step = { kind: 'trailer'; title: string; youtube: string } | { kind: 'intro'; url: string }

/**
 * Plays trailers of other movies and then the server's intro video before a
 * movie. Anything that fails to load is skipped; Skip and Esc move on.
 */
export function PreRoll({ data, movieTitle, onDone }: { data: PreRollData; movieTitle: string; onDone: () => void }) {
  const steps: Step[] = [
    ...data.trailers.map((t): Step => ({ kind: 'trailer', title: t.year ? `${t.title} (${t.year})` : t.title, youtube: t.youtube })),
    ...(data.intro ? [{ kind: 'intro', url: data.intro.url } as Step] : []),
  ]
  const [i, setI] = useState(0)
  const step = steps[i]
  const next = () => setI((n) => n + 1)
  const ytHost = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!step) onDone()
  }, [step, onDone])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onDone()
      else if (e.key === 'Enter' || e.key === 'ArrowRight') setI((n) => n + 1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onDone])

  useEffect(() => {
    if (step?.kind !== 'trailer') return
    let player: YTPlayer | undefined
    let gone = false
    loadYouTube().then((ok) => {
      if (gone) return
      if (!ok || !ytHost.current) return next()
      const el = document.createElement('div')
      ytHost.current.replaceChildren(el)
      player = new window.YT!.Player(el, {
        videoId: step.youtube,
        width: '100%',
        height: '100%',
        host: 'https://www.youtube-nocookie.com',
        playerVars: { autoplay: 1, rel: 0, controls: 0, modestbranding: 1, playsinline: 1, iv_load_policy: 3 },
        events: {
          onStateChange: (e: { data: number }) => e.data === 0 && next(), // ended
          onError: () => next(),
        },
      })
    })
    return () => {
      gone = true
      player?.destroy()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [i])

  if (!step) return <div className="player"><div className="spinner" /></div>
  return (
    <div className="player preroll show-controls">
      {step.kind === 'trailer' ? (
        <div className="preroll-yt" ref={ytHost} />
      ) : (
        <video
          key={step.url}
          className="player-video"
          src={step.url}
          playsInline
          onEnded={next}
          onError={next}
          ref={(v) => {
            // Browsers can block autoplay with sound; play muted rather than sit still.
            if (v && !v.dataset.started) {
              v.dataset.started = '1'
              v.play().catch(() => {
                v.muted = true
                v.play().catch(next)
              })
            }
          }}
        />
      )}
      <div className="preroll-bar">
        <div className="muted small">
          {step.kind === 'trailer' ? <>Trailer · {step.title}</> : <>Up next: {movieTitle}</>}
          {steps.length > 1 && <span> · {i + 1} of {steps.length}</span>}
        </div>
        <div className="preroll-actions">
          {i < steps.length - 1 && (
            <button className="btn btn-glass btn-sm" onClick={next}>Skip</button>
          )}
          <button className="btn btn-primary btn-sm" onClick={onDone}>
            {i < steps.length - 1 ? 'Skip all' : 'Start movie'}
          </button>
        </div>
      </div>
    </div>
  )
}
