// After signing in, the profile picker comes first. The flag lives in
// sessionStorage so a reload doesn't skip it, and a new tab doesn't ask again.

const PICK_PROFILE_KEY = 'bb:pickProfile'

/** Marks that the next page should be the profile picker (after signing in). */
export function askForProfile() {
  try {
    sessionStorage.setItem(PICK_PROFILE_KEY, '1')
  } catch {
    // Private windows can refuse storage; the picker is still in the menu.
  }
}

export function profilePickPending() {
  try {
    return sessionStorage.getItem(PICK_PROFILE_KEY) === '1'
  } catch {
    return false
  }
}

export function donePicking() {
  try {
    sessionStorage.removeItem(PICK_PROFILE_KEY)
  } catch {
    // ignore
  }
}
