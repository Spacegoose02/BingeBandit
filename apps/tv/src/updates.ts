import { NativeEventEmitter, NativeModules } from 'react-native'
import { api } from './api'

interface Updater {
  versionName: string
  versionCode: number
  selfUpdate?: boolean
  setupServers?: string
  canInstall(): Promise<boolean>
  openInstallSettings(): Promise<boolean>
  downloadAndInstall(url: string): Promise<boolean>
}

const AppUpdater: Updater | undefined = NativeModules.AppUpdater

/** The installed app's version, from Android (not package.json). */
export const installed = {
  name: AppUpdater?.versionName ?? 'dev',
  code: AppUpdater?.versionCode ?? 0,
}

/**
 * The addresses of the server this app was downloaded from, when it came from
 * that server's /tv link (the server writes them into the APK). Empty for an
 * APK from GitHub or the Play Store.
 */
export function setupServers(): string[] {
  try {
    const list = JSON.parse(AppUpdater?.setupServers || '{}').servers
    return Array.isArray(list) ? list.filter((u): u is string => typeof u === 'string' && /^https?:\/\//.test(u)) : []
  } catch {
    return []
  }
}

/** False in the Google Play build: Play updates it, so there's no in-app updater. */
export const selfUpdates = AppUpdater?.selfUpdate !== false

export interface AppRelease {
  version: string
  versionCode: number
  url: string
  size: number
  publishedAt: string
  notes?: string
}

/** Asks the server for the newest app release; null when this one is current. */
export async function checkForUpdate(): Promise<AppRelease | null> {
  if (!selfUpdates) return null
  const rel = await api.get<AppRelease | undefined>('/api/apps/android')
  if (!rel || !installed.code || rel.versionCode <= installed.code) return null
  return rel
}

export class NeedsPermission extends Error {}

/**
 * Downloads and opens the installer. Android asks the viewer to confirm; the
 * first time it also needs "install unknown apps" allowed for BingeBandit.
 */
export async function installUpdate(rel: AppRelease, onProgress: (fraction: number) => void): Promise<void> {
  if (!AppUpdater) throw new Error('Updating is only available in the TV app.')
  if (!(await AppUpdater.canInstall())) {
    await AppUpdater.openInstallSettings()
    throw new NeedsPermission('Allow BingeBandit to install apps on the screen that opened, then press Back and choose Update again.')
  }
  const sub = new NativeEventEmitter(NativeModules.AppUpdater).addListener('AppUpdaterProgress', (e: { done: number; total: number }) =>
    onProgress(e.total > 0 ? e.done / e.total : 0),
  )
  try {
    await AppUpdater.downloadAndInstall(rel.url)
  } finally {
    sub.remove()
  }
}
