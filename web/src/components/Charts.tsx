import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { img } from '../api'

// Chart marks use a darker teal than the UI accent: it passes the palette
// validator's lightness band and 3:1 contrast on the dark chart surface.
const MARK = 'var(--chart-mark)'

export interface Column {
  label: string // axis label (may be shown sparsely)
  value: number
  tip: ReactNode // tooltip body
}

function niceMax(v: number): number {
  if (v <= 0) return 1
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p
  return 10 * p
}

// Tick labels: 0 / 0.25 / 2.5 / 1K, never rounding a tick to a different value.
const compact = (n: number) => {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}K`
  return String(Number(n.toFixed(2)))
}

/** Single-series column chart with gridlines, sparse x labels and hover tooltips. */
export function ColumnChart({
  data,
  height = 200,
  labelEvery = 1,
  caption,
  valueLabel,
}: {
  data: Column[]
  height?: number
  labelEvery?: number
  caption: string // describes the chart for screen readers and the table
  valueLabel: string
}) {
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(600)
  const [hover, setHover] = useState<number | null>(null)

  useEffect(() => {
    const el = box.current
    if (!el) return
    const ro = new ResizeObserver(() => setWidth(el.clientWidth))
    ro.observe(el)
    setWidth(el.clientWidth)
    return () => ro.disconnect()
  }, [])

  const pad = { top: 10, right: 8, bottom: 26, left: 40 }
  const plotW = Math.max(width - pad.left - pad.right, 50)
  const plotH = height - pad.top - pad.bottom
  const max = niceMax(Math.max(...data.map((d) => d.value), 0))
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * max)
  const band = plotW / Math.max(data.length, 1)
  const barW = Math.max(Math.min(24, band - 2), 2)
  const y = (v: number) => pad.top + plotH - (v / max) * plotH

  return (
    <figure className="chart" aria-label={caption}>
      <div className="chart-box" ref={box} onMouseLeave={() => setHover(null)}>
        <svg width={width} height={height} role="img" aria-label={caption}>
          {ticks.map((t) => (
            <g key={t}>
              <line x1={pad.left} x2={width - pad.right} y1={y(t)} y2={y(t)} className="chart-grid" />
              <text x={pad.left - 8} y={y(t)} className="chart-tick" textAnchor="end" dominantBaseline="middle">
                {compact(t)}
              </text>
            </g>
          ))}
          {data.map((d, i) => {
            const cx = pad.left + band * i + band / 2
            const h = Math.max(y(0) - y(d.value), d.value > 0 ? 2 : 0)
            const x = cx - barW / 2
            const top = y(0) - h
            const r = Math.min(4, barW / 2, h)
            return (
              <g key={i}>
                {h > 0 && (
                  // Rounded data end, square at the baseline.
                  <path
                    d={`M${x},${y(0)} V${top + r} Q${x},${top} ${x + r},${top} H${x + barW - r} Q${x + barW},${top} ${x + barW},${top + r} V${y(0)} Z`}
                    fill={MARK}
                    opacity={hover === null || hover === i ? 1 : 0.55}
                  />
                )}
                {i % labelEvery === 0 && (
                  <text x={cx} y={height - 8} className="chart-tick" textAnchor="middle">
                    {d.label}
                  </text>
                )}
                {/* Hit target: the whole column band, much bigger than the bar. */}
                <rect x={pad.left + band * i} y={pad.top} width={band} height={plotH} fill="transparent" onMouseEnter={() => setHover(i)} />
              </g>
            )
          })}
        </svg>
        {hover !== null && data[hover] && (
          <div
            className="chart-tip"
            style={{ left: Math.min(Math.max(pad.left + band * hover + band / 2, 70), width - 70), top: Math.max(y(data[hover].value) - 8, 0) }}
          >
            {data[hover].tip}
          </div>
        )}
      </div>
      <details className="chart-table">
        <summary>Show as table</summary>
        <table>
          <thead>
            <tr>
              <th>{caption}</th>
              <th>{valueLabel}</th>
            </tr>
          </thead>
          <tbody>
            {data.map((d, i) => (
              <tr key={i}>
                <td>{d.label}</td>
                <td>{d.value % 1 ? d.value.toFixed(1) : d.value}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>
    </figure>
  )
}

export interface Ranked {
  label: string
  value: number
  display: string // formatted value shown at the bar's end
  href?: string
  poster?: string
}

/** Horizontal ranked bars: label, bar, value at the tip. */
export function RankedBars({ rows, empty = 'No data yet' }: { rows: Ranked[]; empty?: string }) {
  if (!rows.length) return <div className="muted small">{empty}</div>
  const max = Math.max(...rows.map((r) => r.value), 1)
  return (
    <ol className="ranked">
      {rows.map((r, i) => (
        <li key={`${r.label}-${i}`}>
          {r.poster !== undefined && <span className="ranked-poster">{r.poster && <img src={img(r.poster, 'w92')} alt="" loading="lazy" />}</span>}
          <div className="ranked-body">
            <div className="ranked-label">{r.href ? <Link to={r.href}>{r.label}</Link> : r.label}</div>
            <div className="ranked-track">
              <span className="ranked-bar" style={{ width: `${Math.max((r.value / max) * 100, 2)}%` }} />
              <span className="ranked-value">{r.display}</span>
            </div>
          </div>
        </li>
      ))}
    </ol>
  )
}

export function StatTile({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="stat">
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}
