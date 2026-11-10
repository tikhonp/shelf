import { accountCall, useAuth, type AuthStatus } from '../stores/auth'

function decode(value: string): ArrayBuffer {
  const bytes = Uint8Array.from(atob(value.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0))
  return bytes.buffer
}
function encode(value: ArrayBuffer | null): string | null {
  if (!value) return null
  return btoa(String.fromCharCode(...new Uint8Array(value))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}
function serialize(c: PublicKeyCredential) {
  const r = c.response
  const common = { id: c.id, rawId: encode(c.rawId), type: c.type, authenticatorAttachment: c.authenticatorAttachment, clientExtensionResults: c.getClientExtensionResults() }
  if (r instanceof AuthenticatorAttestationResponse) {
    return { ...common, response: { clientDataJSON: encode(r.clientDataJSON), attestationObject: encode(r.attestationObject), transports: r.getTransports() } }
  }
  const assertion = r as AuthenticatorAssertionResponse
  return { ...common, response: { clientDataJSON: encode(r.clientDataJSON), authenticatorData: encode(assertion.authenticatorData), signature: encode(assertion.signature), userHandle: encode(assertion.userHandle) } }
}

// Decode explicitly for browsers that do not expose parseCreationOptionsFromJSON.
export async function addPasskey(name: string) {
  const data = await accountCall<{ publicKey: PublicKeyCredentialCreationOptions }>('/passkeys/begin')
  const pk = data.publicKey
  pk.challenge = decode(pk.challenge as unknown as string)
  pk.user.id = decode(pk.user.id as unknown as string)
  pk.excludeCredentials = pk.excludeCredentials?.map((c) => ({ ...c, id: decode(c.id as unknown as string) }))
  const credential = await navigator.credentials.create({ publicKey: pk }) as PublicKeyCredential | null
  if (!credential) throw new Error('Passkey registration was canceled')
  await accountCall(`/passkeys/finish?name=${encodeURIComponent(name)}`, serialize(credential))
}
// passkeyLogin applies the answer at once unless apply is false, as login does.
// Aborting the signal cancels the browser's request, which then fails with an AbortError.
export async function passkeyLogin(apply = true, signal?: AbortSignal): Promise<AuthStatus> {
  const data = await accountCall<{ publicKey: PublicKeyCredentialRequestOptions }>('/passkey/begin')
  const pk = data.publicKey
  pk.challenge = decode(pk.challenge as unknown as string)
  pk.allowCredentials = pk.allowCredentials?.map((c) => ({ ...c, id: decode(c.id as unknown as string) }))
  const credential = await navigator.credentials.get({ publicKey: pk, signal }) as PublicKeyCredential | null
  if (!credential) throw new Error('Passkey sign-in was canceled')
  const v = await accountCall<AuthStatus>('/passkey/finish', serialize(credential))
  if (apply) useAuth().signedIn(v)
  return v
}
export const supportsPasskeys = () => window.isSecureContext && typeof PublicKeyCredential !== 'undefined'
