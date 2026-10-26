import AsyncStorage from '@react-native-async-storage/async-storage'

// Settings that belong to this device rather than the account. They load
// when the app starts, long before a player needs them.

const HUE_KEY = 'bb.hueSync'
const HUE_AREA_KEY = 'bb.hueArea'
let hueSync = true
let area = ''

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
