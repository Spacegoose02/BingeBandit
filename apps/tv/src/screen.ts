import { useEffect } from 'react'
import { NativeModules } from 'react-native'
import { isPhone } from './theme'

const Screen = NativeModules.Screen as { setLandscape(on: boolean): void; setImmersive?(on: boolean): void } | undefined

/**
 * Turns a phone sideways while the calling screen (a player) is open, and hides
 * the navigation bar (a tablet's taskbar too) so the video gets the whole
 * screen. TVs are already landscape and have no bars.
 */
export function useLandscape() {
  useEffect(() => {
    if (!isPhone) return
    Screen?.setLandscape(true)
    Screen?.setImmersive?.(true)
    return () => {
      Screen?.setLandscape(false)
      Screen?.setImmersive?.(false)
    }
  }, [])
}
