/** Passkeys (WebAuthn): the server's JSON options in, the browser's credential back out as JSON. */

const toBytes = (b64url: string) => {
  const b64 = b64url.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(b64url.length / 4) * 4, '=')
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
}
const toB64url = (buf: ArrayBuffer | null | undefined) =>
  buf ? btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') : undefined

type Descriptor = { id: string; type: string; transports?: string[] }

/** Whether this page can use passkeys (they need https://, or localhost). */
export function passkeysSupported(): boolean {
  return window.isSecureContext && !!window.PublicKeyCredential && !!navigator.credentials
}

function credentialJSON(c: PublicKeyCredential) {
  const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse
  return {
    id: c.id,
    rawId: toB64url(c.rawId),
    type: c.type,
    authenticatorAttachment: c.authenticatorAttachment ?? undefined,
    clientExtensionResults: c.getClientExtensionResults(),
    response: {
      clientDataJSON: toB64url(r.clientDataJSON),
      attestationObject: 'attestationObject' in r ? toB64url(r.attestationObject) : undefined,
      transports: 'getTransports' in r ? r.getTransports() : undefined,
      authenticatorData: 'authenticatorData' in r ? toB64url(r.authenticatorData) : undefined,
      signature: 'signature' in r ? toB64url(r.signature) : undefined,
      userHandle: 'userHandle' in r ? toB64url(r.userHandle) : undefined,
    },
  }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Options = any

/** Asks the browser to make a passkey from the server's creation options. */
export async function createPasskey(options: Options) {
  const publicKey: PublicKeyCredentialCreationOptions = {
    ...options,
    challenge: toBytes(options.challenge),
    user: { ...options.user, id: toBytes(options.user.id) },
    excludeCredentials: (options.excludeCredentials ?? []).map((d: Descriptor) => ({ ...d, id: toBytes(d.id) })),
  }
  const c = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null
  if (!c) throw new Error('No passkey was made.')
  return credentialJSON(c)
}

/** Asks the browser to sign in with any passkey it has for this server. */
export async function getPasskey(options: Options) {
  const publicKey: PublicKeyCredentialRequestOptions = {
    ...options,
    challenge: toBytes(options.challenge),
    allowCredentials: (options.allowCredentials ?? []).map((d: Descriptor) => ({ ...d, id: toBytes(d.id) })),
  }
  const c = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential | null
  if (!c) throw new Error('No passkey was chosen.')
  return credentialJSON(c)
}

/** Turns the browser's "cancelled" into nothing and other errors into a sentence. */
export function passkeyErrorMessage(err: unknown): string {
  if (err instanceof DOMException && (err.name === 'NotAllowedError' || err.name === 'AbortError')) return ''
  if (err instanceof DOMException && err.name === 'InvalidStateError') return 'This device already has a passkey for your account.'
  if (err instanceof DOMException && err.name === 'SecurityError') return "Passkeys don't work on this address. Open the server by its https:// address."
  return (err as Error).message
}
