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
