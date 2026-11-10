<script setup lang="ts">
import { computed, nextTick, onMounted, onScopeDispose, ref } from 'vue'
import { display, failed, type SignState } from '../lib/account'
import { passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { useAuth } from '../stores/auth'
import { useEditor } from '../stores/editor'
import { useTheme } from '../stores/theme'
import PasskeySlide from './PasskeySlide.vue'

// Sign in again, over the inert app, when a session ends inside it. The same account resumes; another reloads the page.
const auth = useAuth()
const theme = useTheme()
const editor = useEditor()
const known = auth.previousName
const username = ref(known)
// other is true after "Not <name>?", when the user types another username.
const other = ref(!known)
const password = ref('')
const state = ref<SignState>('ended')
const dialog = ref<HTMLDialogElement>()
const usernameEl = ref<HTMLInputElement>()
const passwordEl = ref<HTMLInputElement>()
const passkeys = supportsPasskeys()
const busy = computed(() => state.value === 'busy' || state.value === 'waiting')
const shown = computed(() => display(state.value, { name: username.value, kept: editor.kept }))
// abort cancels the passkey request the slide started.
let abort: AbortController | null = null
onScopeDispose(() => abort?.abort())

onMounted(() => {
  dialog.value?.showModal()
  void nextTick(() => (other.value ? usernameEl : passwordEl).value?.focus())
})

function notMe() {
  other.value = true
  username.value = ''
  void nextTick(() => usernameEl.value?.focus())
}
async function signIn(passkey = false) {
  if (busy.value) return
  state.value = passkey ? 'waiting' : 'busy'
  if (passkey) abort = new AbortController()
  try {
    if (passkey) await passkeyLogin(true, abort!.signal)
    else await auth.login(username.value, password.value)
  } catch (e) {
    state.value = failed(e)
    if (!passkey && state.value === 'wrong') void nextTick(() => passwordEl.value?.focus())
  } finally { password.value = ''; abort = null }
}
// slide runs for the Passkey slide. OFF to ON asks for a passkey, and ON to OFF cancels the request.
function slide() {
  if (state.value === 'waiting') abort?.abort()
  else void signIn(true)
}
</script>

<template>
  <dialog ref="dialog" class="account-dialog rs" :class="{ day: theme.day }" aria-labelledby="account-login-title" @cancel.prevent>
    <section class="win">
      <div class="tb"><h1 id="account-login-title">Sign in again</h1><span class="stripes"></span></div>
      <form class="account-body" @submit.prevent="signIn()">
        <div :key="shown.tone === 'err' ? 'alert' : 'status'" class="lcd account-lcd" :class="shown.tone" :role="shown.tone === 'err' ? 'alert' : 'status'"><div v-for="(l, i) in shown.lines" :key="i" class="lcd-l">{{ l }}</div></div>
        <div class="account-user">
          <label class="lbl-f">Username<input ref="usernameEl" v-model="username" class="input" name="username" autocomplete="username webauthn" autocapitalize="none" spellcheck="false" required maxlength="32" :readonly="!other || busy" /></label>
          <button v-if="!other" class="link sm account-not" type="button" @click="notMe">Not {{ known }}?</button>
        </div>
        <label class="lbl-f">Password<input ref="passwordEl" v-model="password" class="input" name="password" type="password" autocomplete="current-password" required :readonly="busy" /></label>
        <button class="key sun block" type="submit" :class="{ pressed: state === 'busy' }" :disabled="busy">Sign in</button>
        <div v-if="passkeys" class="account-pk">
          <PasskeySlide row :on="state === 'waiting'" :disabled="state === 'busy'" @toggle="slide" />
        </div>
      </form>
    </section>
  </dialog>
</template>

<style>
.account-dialog.rs { position: fixed; inset: 0; margin: auto; width: min(440px, calc(100% - 24px)); height: fit-content; min-height: 0; max-height: calc(100dvh - 24px); padding: 0; border: none; background: transparent; color: var(--text); }
.account-dialog::backdrop { background: rgba(4, 6, 10, .62); }
.account-dialog.day::backdrop { background: rgba(30, 26, 18, .35); }
.rs .account-dialog .tb h1, .account-dialog.rs .tb h1 { display: flex; align-items: center; gap: 8px; }
.account-dialog.rs .account-body { display: flex; flex-direction: column; gap: 12px; padding: 16px; }
.account-dialog.rs .account-lcd { flex: none; min-height: 58px; justify-content: center; }
.account-dialog.rs .account-user { display: flex; flex-direction: column; gap: 6px; }
.account-dialog.rs .account-not { align-self: flex-start; }
.account-dialog.rs .lbl-f .input { text-transform: none; letter-spacing: normal; font-family: var(--font); font-weight: 400; }
.account-dialog.rs .account-pk { border-top: 1px solid var(--line); }
/* The slide's row is a switch, not a list row, so it takes no hover background. */
.account-dialog.rs .account-pk .slide.row:hover { background: transparent; }
/* On the phone the window sits at the top, so the keyboard never covers its keys. */
@media (max-width: 719px) {
  .account-dialog.rs { margin: 12px auto auto; }
}
</style>
