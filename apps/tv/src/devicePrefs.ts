import AsyncStorage from '@react-native-async-storage/async-storage'

// Settings that belong to this device rather than the account. They load
// when the app starts, long before a player needs them.

const HUE_KEY = 'bb.hueSync'
const HUE_AREA_KEY = 'bb.hueArea'
const HUE_TYPE_KEY = 'bb.hueType'
let hueSync = true
let area = ''
let type: HueType = ''

AsyncStorage.getItem(HUE_KEY).then(
  (v) => {
    if (v !== null) hueSync = v !== '0'
  },
  () => undefined,
)
AsyncStorage.getItem(HUE_AREA_KEY).then(
  (v) => {
    area = v ?? ''
  },
  () => undefined,
)
AsyncStorage.getItem(HUE_TYPE_KEY).then(
  (v) => {
    if (v === 'sync' || v === 'theater') type = v
  },
  () => undefined,
)

/** Whether Hue Sync may follow this device's player (on unless switched off). */
export function hueSyncOn(): boolean {
  return hueSync
}

export function setHueSync(on: boolean) {
  hueSync = on
  AsyncStorage.setItem(HUE_KEY, on ? '1' : '0').catch(() => undefined)
}

/** The Hue room or zone this device's lights are in; '' for every chosen light. */
export function hueArea(): string {
  return area
}

export function setHueArea(id: string) {
  area = id
  AsyncStorage.setItem(HUE_AREA_KEY, id).catch(() => undefined)
}

/** Sync with the picture or Theater on this device; '' until picked, which uses the profile's. */
export type HueType = '' | 'sync' | 'theater'

export function hueType(): HueType {
  return type
}

export function setHueType(t: HueType) {
  type = t
  AsyncStorage.setItem(HUE_TYPE_KEY, t).catch(() => undefined)
}

// Where the TV menu sits: down the left side, collapsed to icons until
// focus moves into it (like Plex), or across the top.
const MENU_KEY = 'bb.menuPosition'
export type MenuPosition = 'side' | 'top'
let menuPos: MenuPosition = 'side'
const menuLoaded = AsyncStorage.getItem(MENU_KEY).then(
  (v) => {
    if (v === 'top' || v === 'side') menuPos = v
  },
  () => undefined,
)

export function menuPosition(): MenuPosition {
  return menuPos
}

/** Resolves once the saved menu position is known. */
export function menuPositionLoaded(): Promise<void> {
  return menuLoaded
}

export function setMenuPosition(p: MenuPosition) {
  menuPos = p
  AsyncStorage.setItem(MENU_KEY, p).catch(() => undefined)
}

// This device's own id, made the first time it's needed, so the server can
// keep settings per TV (Game Lights' switch and room).
const DEVICE_KEY = 'bb.deviceId'
const deviceIdLoaded = AsyncStorage.getItem(DEVICE_KEY).then(
  (v) => v,
  () => null,
)
let devId = ''

export async function deviceId(): Promise<string> {
  if (devId) return devId
  devId = (await deviceIdLoaded) || Math.random().toString(36).slice(2, 10) + Date.now().toString(36)
  AsyncStorage.setItem(DEVICE_KEY, devId).catch(() => undefined)
  return devId
}
