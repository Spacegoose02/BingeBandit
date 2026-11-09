import { NativeModules } from 'react-native'

/** Opening the app when the TV starts (android/.../AutoLaunch.kt). */
export interface AutoLaunchState {
  on: boolean
  needsPermission: boolean // Android 10+ wants "display over other apps" first
  canAsk: boolean // this build asks for that permission (the Play build doesn't)
}

const native = NativeModules.AutoLaunch as
  | { state(): Promise<AutoLaunchState>; setOn(on: boolean): void; openPermission(): Promise<boolean> }
  | undefined

export const autoLaunchState = (): Promise<AutoLaunchState | null> => (native ? native.state() : Promise.resolve(null))
export const setAutoLaunch = (on: boolean) => native?.setOn(on)
/** Opens Android's "display over other apps" page; false when this device hides it. */
export const openAutoLaunchPermission = (): Promise<boolean> => (native ? native.openPermission() : Promise.resolve(false))

/** Grants the permission on devices that hide its page (Google TV, Chromecast). */
export const adbGrant = 'adb shell appops set com.bingebandit.tv SYSTEM_ALERT_WINDOW allow'
