import { useEffect } from 'react'
import { NativeModules } from 'react-native'
import { isPhone } from './theme'

const Screen = NativeModules.Screen as { setLandscape(on: boolean): void } | undefined

/** Turns a phone sideways while the calling screen (a player) is open. TVs are already landscape. */
export function useLandscape() {
  useEffect(() => {
    if (!isPhone) return
    Screen?.setLandscape(true)
    return () => Screen?.setLandscape(false)
  }, [])
}
