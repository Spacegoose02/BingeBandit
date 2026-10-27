// "Who's watching?" comes first whenever BingeBandit opens in a new tab and
// after signing in, unless the account picked a member to open as. The flag
// lives in sessionStorage so a reload doesn't ask again but a new tab does.

const PICKED_KEY = 'bb:profilePicked'

/** Marks that the next page should be the profile picker (after signing in). */
export function askForProfile() {
  try {
    sessionStorage.removeItem(PICKED_KEY)
  } catch {
    // Private windows can refuse storage; the picker is still in the menu.
  }
}

export function profilePickPending() {
  try {
    return sessionStorage.getItem(PICKED_KEY) !== '1'
  } catch {
    return false
  }
}

export function donePicking() {
  try {
    sessionStorage.setItem(PICKED_KEY, '1')
  } catch {
    // ignore
  }
}

// A member can have this browser open straight as them, skipping the picker.
// Kept per browser (localStorage) and only honoured while still signed in as
// that member, so after switching to someone else the picker (and its PIN)
// comes back.
const STAY_KEY = 'bb:stayAs'

export function stayingAs(): number {
  try {
    return Number(localStorage.getItem(STAY_KEY)) || 0
  } catch {
    return 0
  }
}

export function setStayingAs(memberId: number) {
  try {
    if (memberId) localStorage.setItem(STAY_KEY, String(memberId))
    else localStorage.removeItem(STAY_KEY)
  } catch {
    // Private windows can refuse storage.
  }
}
