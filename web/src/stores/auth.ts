import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { ApiError } from '../api/client'

export interface Account { id: number; username: string }
export interface AuthStatus { provider: 'none' | 'simple'; account: Account | null; setup_needed: boolean }
export interface Passkey { id: string; name: string; created_at: string; last_used_at: string | null }

// Account calls remain available while normal application requests are paused.
export async function accountCall<T>(path: string, body?: unknown, method = 'POST'): Promise<T> {
  const res = await fetch(`/api/auth${path}`, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: 'no-store',
  })
  const data = await res.json()
  if (!res.ok) {
    if (res.status === 401 && path !== '') void useAuth().check().catch(() => {})
    throw new ApiError(res.status, data.error || res.statusText)
  }
  return data as T
}

export const useAuth = defineStore('auth', () => {
  const ready = ref(false)
  const provider = ref<'none' | 'simple'>('none')
  const account = ref<Account | null>(null)
  const setupNeeded = ref(false)
  const error = ref('')
  const mounted = ref(false)
  const previousID = ref<number | null>(null)
  // previousName fills the username when a session ends inside the app.
  const previousName = ref('')
  const locked = computed(() => ready.value && provider.value === 'simple' && !account.value)
  let checking: Promise<void> | null = null

  function apply(v: AuthStatus) {
    if (v.account && previousID.value !== null && v.account.id !== previousID.value) {
      window.location.reload()
      return
    }
    provider.value = v.provider
    account.value = v.account
    setupNeeded.value = v.setup_needed
    ready.value = true
    if (v.account) { previousID.value = v.account.id; previousName.value = v.account.username }
    if (v.provider === 'none' || v.account) mounted.value = true
  }
  async function check() {
    if (checking) return checking
    checking = (async () => {
      try { apply(await accountCall<AuthStatus>('', undefined, 'GET')); error.value = '' }
      catch (e) { error.value = e instanceof Error ? e.message : 'Cannot check account access'; throw e }
      finally { checking = null }
    })()
    return checking
  }
  function expired() {
    if (provider.value !== 'simple') return
    account.value = null
  }
  function signedIn(v: AuthStatus) {
    apply(v)
  }
  // login applies the answer at once, unless the caller first plays the power-up and applies it with signedIn.
  async function login(username: string, password: string, apply = true): Promise<AuthStatus> {
    const v = await accountCall<AuthStatus>('/login', { username, password })
    if (apply) signedIn(v)
    return v
  }
  async function logout() {
    await accountCall('/logout')
    window.location.reload()
  }
  return { ready, provider, account, setupNeeded, error, mounted, previousID, previousName, locked, check, expired, signedIn, login, logout }
})
