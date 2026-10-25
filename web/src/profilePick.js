// "Who's watching?" comes first whenever BingeBandit opens in a new tab and
// after signing in, unless the account picked a member to open as. The flag
// lives in sessionStorage so a reload doesn't ask again but a new tab does.
const PICKED_KEY = 'bb:profilePicked';
/** Marks that the next page should be the profile picker (after signing in). */
export function askForProfile() {
    try {
        sessionStorage.removeItem(PICKED_KEY);
    }
    catch {
        // Private windows can refuse storage; the picker is still in the menu.
    }
}
export function profilePickPending() {
    try {
        return sessionStorage.getItem(PICKED_KEY) !== '1';
    }
    catch {
        return false;
    }
}
export function donePicking() {
    try {
        sessionStorage.setItem(PICKED_KEY, '1');
    }
    catch {
        // ignore
    }
}
