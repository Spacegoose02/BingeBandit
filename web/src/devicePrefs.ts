// Settings that belong to this browser rather than the account.

const HUE_KEY = "bb.hueSync";

/** Whether Hue Sync may follow this browser's player (on unless switched off). */
export function hueSyncOn(): boolean {
  return localStorage.getItem(HUE_KEY) !== "0";
}

export function setHueSync(on: boolean) {
  localStorage.setItem(HUE_KEY, on ? "1" : "0");
}
