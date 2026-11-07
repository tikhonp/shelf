<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { errorText } from '../api/client'
import { passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { useAuth } from '../stores/auth'

const auth = useAuth()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)
const dialog = ref<HTMLDialogElement>()
const passkeys = supportsPasskeys()
onMounted(() => dialog.value?.showModal())
async function signIn(passkey = false) {
  busy.value = true; error.value = ''
  try {
    if (passkey) await passkeyLogin()
    else await auth.login(username.value, password.value)
  } catch (e) { error.value = e instanceof DOMException && e.name === 'NotAllowedError' ? 'Passkey sign-in was canceled. You can use your password.' : errorText(e) }
  finally { password.value = ''; busy.value = false }
}
</script>

<template>
  <dialog ref="dialog" class="account-dialog rs" aria-labelledby="account-login-title" @cancel.prevent>
    <section class="win">
      <div class="tb"><h1 id="account-login-title">Sign in to Shelf</h1><span class="stripes"></span></div>
      <div class="account-body">
        <p v-if="auth.setupNeeded">No accounts exist. Run <code>shelf manage users add &lt;username&gt;</code> in the server's terminal.</p>
        <form v-else class="account-form" @submit.prevent="signIn()">
          <label>Username<input v-model="username" name="username" autocomplete="username webauthn" autocapitalize="none" spellcheck="false" required maxlength="32" autofocus /></label>
          <label>Password<input v-model="password" name="password" type="password" autocomplete="current-password" required /></label>
          <p v-if="error" role="alert">{{ error }}</p>
          <button class="key sun" type="submit" :disabled="busy">Sign in to Shelf</button>
          <button v-if="passkeys" class="key" type="button" :disabled="busy" @click="signIn(true)">Sign in with a passkey</button>
        </form>
      </div>
    </section>
  </dialog>
</template>

<style>
.account-dialog.rs { position: fixed; inset: 0; margin: auto; width: min(440px, calc(100% - 24px)); height: fit-content; min-height: 0; max-height: calc(100dvh - 24px); padding: 0; border: none; background: transparent; color: var(--text); }
.account-dialog::backdrop { background: rgba(0, 0, 0, .8); }
.rs .account-body { padding: 20px; }
.rs .account-form { display: grid; gap: 16px; }
.rs .account-form label { display: grid; gap: 6px; }
.rs .account-form input { width: 100%; box-sizing: border-box; padding: 10px; border: 1px solid var(--ink); border-radius: 4px; background: var(--lcd); color: var(--lcd-text); font: inherit; font-size: 16px; }
</style>
