<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { ApiError, errorText } from '../api/client'
import { passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { useAuth } from '../stores/auth'
import { useTheme } from '../stores/theme'

const auth = useAuth()
const theme = useTheme()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)
const dialog = ref<HTMLDialogElement>()
const passwordEl = ref<HTMLInputElement>()
const passkeys = supportsPasskeys()
onMounted(() => dialog.value?.showModal())

// Shelf words its own failures, because the server writes its sentence for logs and API clients.
function failure(e: unknown): string {
  if (e instanceof DOMException && e.name === 'NotAllowedError') return 'Passkey sign-in was canceled. You can use your password.'
  if (e instanceof ApiError && e.status === 401) return 'Wrong username or password.'
  if (e instanceof ApiError && e.status === 429) return 'Too many tries. Try again later.'
  return errorText(e)
}
async function signIn(passkey = false) {
  busy.value = true; error.value = ''
  try {
    if (passkey) await passkeyLogin()
    else await auth.login(username.value, password.value)
  } catch (e) {
    error.value = failure(e)
    if (!passkey && e instanceof ApiError && e.status === 401) void nextTick(() => passwordEl.value?.focus())
  } finally { password.value = ''; busy.value = false }
}
</script>

<template>
  <dialog ref="dialog" class="account-dialog rs" :class="{ day: theme.day, over: auth.mounted }" aria-labelledby="account-login-title" @cancel.prevent>
    <section class="win">
      <div class="tb"><h1 id="account-login-title">{{ auth.mounted ? 'Sign in again' : 'Sign in to Shelf' }}</h1><span class="stripes"></span></div>
      <div class="account-body">
        <p v-if="auth.setupNeeded">No accounts exist. Run <code>shelf manage users add &lt;username&gt;</code> in the server's terminal.</p>
        <form v-else class="account-form" @submit.prevent="signIn()">
          <label class="lbl-f">Username<input v-model="username" class="input" name="username" autocomplete="username webauthn" autocapitalize="none" spellcheck="false" required maxlength="32" autofocus /></label>
          <label class="lbl-f">Password<input ref="passwordEl" v-model="password" class="input" name="password" type="password" autocomplete="current-password" required /></label>
          <p v-if="error" class="bad-text bold" role="alert">{{ error }}</p>
          <div class="account-keys">
            <button class="key sun" type="submit" :disabled="busy">Sign in</button>
            <button v-if="passkeys" class="key" type="button" :disabled="busy" @click="signIn(true)">Sign in with a passkey</button>
          </div>
        </form>
      </div>
    </section>
  </dialog>
</template>

<style>
.account-dialog.rs { position: fixed; inset: 0; margin: auto; width: min(440px, calc(100% - 24px)); height: fit-content; min-height: 0; max-height: calc(100dvh - 24px); padding: 0; border: none; background: transparent; color: var(--text); }
/* Before the app mounts, the switched-off console behind the window is the backdrop. Over the app, a scrim dims it. */
.account-dialog::backdrop { background: transparent; }
.account-dialog.over::backdrop { background: rgba(4, 6, 10, .62); }
.account-dialog.over.day::backdrop { background: rgba(30, 26, 18, .35); }
.rs .account-body { padding: 20px; }
.rs .account-form { display: grid; gap: 16px; }
.rs .account-form .lbl-f .input { text-transform: none; letter-spacing: normal; font-family: var(--font); font-weight: 400; }
.rs .account-keys { display: flex; flex-wrap: wrap; gap: 10px; }
.rs .account-keys .key.sun { flex: 1 1 140px; }
/* On the phone the window sits at the top, so the keyboard never covers its keys. */
@media (max-width: 719px) {
  .account-dialog.rs { margin: 12px auto auto; }
}
</style>
