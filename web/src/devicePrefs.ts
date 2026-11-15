// Settings that belong to this browser rather than the account.

const HUE_KEY = "bb.hueSync";

/** Whether Hue Sync may follow this browser's player (on unless switched off). */
export function hueSyncOn(): boolean {
  return localStorage.getItem(HUE_KEY) !== "0";
}

export function setHueSync(on: boolean) {
  localStorage.setItem(HUE_KEY, on ? "1" : "0");
}

const HUE_AREA_KEY = "bb.hueArea";

/** The Hue room or zone this browser's lights are in; "" for every chosen light. */
export function hueArea(): string {
  try {
    return localStorage.getItem(HUE_AREA_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setHueArea(id: string) {
  localStorage.setItem(HUE_AREA_KEY, id);
}

const STILL_WATCHING_KEY = "bb.stillWatchingAfter";

/**
 * "Are you still watching?": how many episodes may start by themselves in a
 * row before the player asks; 0 never asks. 3 until changed in this browser
 * (or this Samsung/LG TV or Windows app), since it depends on the screen.
 */
export function stillWatchingAfter(): number {
  try {
    const v = localStorage.getItem(STILL_WATCHING_KEY);
    const n = Number(v);
    if (v !== null && Number.isInteger(n) && n >= 0) return n;
  } catch {
    /* private window */
  }
  return 3;
}

export function setStillWatchingAfter(n: number) {
  try {
    localStorage.setItem(STILL_WATCHING_KEY, String(n));
  } catch {
    /* private window */
  }
}
