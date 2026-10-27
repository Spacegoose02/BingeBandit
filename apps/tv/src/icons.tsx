import React from 'react'
import Svg, { Circle, Path, Rect } from 'react-native-svg'

export type IconName = 'home' | 'search' | 'movies' | 'shows' | 'more' | 'sports' | 'live' | 'settings' | 'pin' | 'pinned' | 'discover'

/** Line icons for the side menu, drawn on a 24-unit grid. */
export function Icon({ name, size, color }: { name: IconName; size: number; color: string }) {
  const line = { stroke: color, strokeWidth: 2, fill: 'none', strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }
  let body: React.ReactNode
  switch (name) {
    case 'home':
      body = <Path d="M3 11 12 3l9 8M5 9.5V21h5v-6h4v6h5V9.5" {...line} />
      break
    case 'discover':
      body = (
        <>
          <Circle cx={12} cy={12} r={9} {...line} />
          <Path d="m15.5 8.5-2 5-5 2 2-5z" {...line} />
        </>
      )
      break
    case 'search':
      body = (
        <>
          <Circle cx={10.5} cy={10.5} r={6.5} {...line} />
          <Path d="m15.5 15.5 5 5" {...line} />
        </>
      )
      break
    case 'movies':
      body = (
        <>
          <Rect x={4} y={3} width={16} height={18} rx={2} {...line} />
          <Path d="M8 3v18M16 3v18M4 8h4M4 12h4M4 16h4M16 8h4M16 12h4M16 16h4" {...line} />
        </>
      )
      break
    case 'shows':
      body = (
        <>
          <Rect x={2.5} y={5} width={19} height={13} rx={2} {...line} />
          <Path d="M8 21h8M9 2l3 3 3-3" {...line} />
        </>
      )
      break
    case 'more':
      body = <Path d="M4 6h16M4 12h16M4 18h16" {...line} />
      break
    case 'sports':
      body = (
        <>
          <Circle cx={12} cy={12} r={9} {...line} />
          <Path d="M12 3v18M3 12h18M5.6 5.6c3.5 3.5 3.5 9.3 0 12.8M18.4 5.6c-3.5 3.5-3.5 9.3 0 12.8" {...line} />
        </>
      )
      break
    case 'live':
      body = (
        <>
          <Rect x={2.5} y={7} width={19} height={13} rx={2} {...line} />
          <Path d="m7 2 5 5 5-5" {...line} />
          <Circle cx={12} cy={13.5} r={2} fill={color} />
        </>
      )
      break
    case 'pin':
    case 'pinned':
      // A push pin: filled when pinned, an outline when not.
      body = <Path d="M9 3h6l-1 6 3 3v2.5H7V12l3-3-1-6ZM12 14.5V21" {...line} fill={name === 'pinned' ? color : 'none'} />
      break
    case 'settings':
      body = (
        <>
          <Circle cx={12} cy={12} r={3} {...line} />
          <Path
            d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1"
            {...line}
          />
        </>
      )
      break
  }
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24">
      {body}
    </Svg>
  )
}
