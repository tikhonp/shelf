<script setup lang="ts">
import { computed, nextTick, onScopeDispose, ref } from 'vue'
import { useRoute } from 'vue-router'
import { display, failed, reducedMotion, type SignState } from '../lib/account'
import { useNow } from '../lib/clock'
import { clock } from '../lib/format'
import { passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { useKeyboard, usePhone } from '../lib/viewport'
import { useAuth, type AuthStatus } from '../stores/auth'
import { useTheme } from '../stores/theme'
import KeySwitch from './KeySwitch.vue'
import PasskeySlide from './PasskeySlide.vue'

// Sign-in before the app mounts: only its unit, with no menubar, title bar or dock.
// The display names the app and shows the state, so the unit has no plate or lamp.
const auth = useAuth()
const theme = useTheme()
const phone = usePhone()
const keyboard = useKeyboard()
const route = useRoute()
const now = useNow()
const passkeys = supportsPasskeys()

const username = ref('')
const password = ref('')
const passwordEl = ref<HTMLInputElement>()
const state = ref<SignState>('out')
// on is where the key switch points. It turns to ON only when a passkey signs in.
const on = ref(false)
// lit counts the lamps the power-up has lit.
const lit = ref(0)
// abort cancels the passkey request the slide started.
let abort: AbortController | null = null

const openings: [string, string][] = [['/library', 'THE LIBRARY'], ['/inbox', 'THE INBOX'], ['/search', 'SEARCH'], ['/downloads', 'DOWNLOADS'], ['/status', 'STATUS']]
const opening = computed(() => (openings.find(([to]) => route.path.startsWith(to)) ?? openings[0]!)[1])

const phase = computed<SignState>(() => {
  if (!auth.ready) return auth.error ? 'noanswer' : 'checking'
  if (auth.setupNeeded) return 'setup'
  return state.value
})
const shown = computed(() => {
  const d = display(phase.value, { name: username.value, passkeys })
  if (phase.value === 'hello') d.lines[1] = `OPENING ${opening.value}`
  return d
})
const form = computed(() => auth.ready && !auth.setupNeeded)
const busy = computed(() => state.value === 'busy' || state.value === 'waiting' || state.value === 'hello')
const slideOn = computed(() => state.value === 'waiting' || on.value)

const timers: ReturnType<typeof setTimeout>[] = []
const wait = (ms: number) => new Promise<void>((resolve) => timers.push(setTimeout(resolve, ms)))
onScopeDispose(() => { timers.forEach(clearTimeout); abort?.abort() })

// powerUp lights the unit's three lamps 260ms apart and starts the clock, then mounts the app.
async function powerUp(v: AuthStatus, passkey: boolean) {
  state.value = 'hello'
  on.value = passkey
  if (reducedMotion()) { auth.signedIn(v); return }
  for (let i = 1; i <= 3; i++) { await wait(260); lit.value = i }
  await wait(260)
  auth.signedIn(v)
}

async function signIn(passkey = false) {
  if (busy.value) return
  state.value = passkey ? 'waiting' : 'busy'
  if (passkey) abort = new AbortController()
  try {
    const v = passkey ? await passkeyLogin(false, abort!.signal) : await auth.login(username.value, password.value, false)
    if (!passkey) password.value = ''
    if (passkey && v.account) username.value = v.account.username
    await powerUp(v, passkey)
  } catch (e) {
    state.value = failed(e)
    if (!passkey) {
      password.value = ''
      if (state.value === 'wrong') void nextTick(() => passwordEl.value?.focus())
    }
  } finally { abort = null }
}
// slide runs for both the slide and the key switch. OFF to ON asks for a passkey, and ON to OFF cancels the request.
function slide() {
  if (state.value === 'waiting') abort?.abort()
  else void signIn(true)
}
const reload = () => window.location.reload()
</script>

<template>
  <div class="rs desk acct-desk" :class="{ day: theme.day, console: !phone, 'acct-phone': phone }">
    <main :class="phone ? 'stack acct-stack' : 'acct-main'" :style="phone ? { bottom: `${keyboard + 10}px` } : undefined" :data-keyboard="phone && keyboard > 0 || undefined">
      <div v-if="!phone" class="win unit acct-blank" aria-hidden="true"><div class="unit-body"><span class="vents"></span></div></div>

      <section class="win acct-unit" :class="{ unit: !phone }" aria-labelledby="acct-h">
        <h1 id="acct-h" class="sr">Sign in to Music Shelf</h1>
        <div :class="phone ? 'vscroll acct-body' : 'unit-body acct-body'">
          <div v-if="!phone && form && passkeys" class="acct-left">
            <KeySwitch :on="on" press @press="slide" />
            <PasskeySlide class="acct-slide" :on="slideOn" :disabled="state === 'busy' || state === 'hello'" @toggle="slide" />
          </div>

          <div class="acct-right">
            <div :key="shown.tone === 'err' ? 'alert' : 'status'" class="lcd acct-lcd" :class="shown.tone" :role="shown.tone === 'err' ? 'alert' : 'status'">
              <div class="lcd-l dim">MUSIC SHELF</div>
              <div v-for="(l, i) in shown.lines" :key="i" class="lcd-l">{{ l }}</div>
            </div>
            <div v-if="phase === 'hello'" class="acct-lamps" aria-hidden="true">
              <span v-for="i in 3" :key="i" class="led" :class="lit >= i ? 'ok' : 'off'"></span>
              <span class="vfd num">{{ clock(now) }}</span>
            </div>
            <form v-else-if="form" class="acct-form" @submit.prevent="signIn()">
              <label class="lbl-f">Username<input v-model="username" class="input" name="username" autocomplete="username webauthn" autocapitalize="none" spellcheck="false" required maxlength="32" autofocus :readonly="busy" /></label>
              <label class="lbl-f">Password<input ref="passwordEl" v-model="password" class="input" name="password" type="password" autocomplete="current-password" required :readonly="busy" /></label>
              <button class="key sun lg block" type="submit" :class="{ pressed: state === 'busy' }" :disabled="busy">Sign in</button>
            </form>
            <template v-else-if="phase === 'setup'">
              <p class="sm acct-p">Run this in the server's terminal, then reload the page.</p>
              <pre class="acct-term"><span class="ts">$</span> shelf manage users add &lt;username&gt;</pre>
              <button class="key block" type="button" @click="reload">Reload</button>
            </template>
            <button v-else-if="phase === 'noanswer'" class="key sun lg block" type="button" @click="auth.check().catch(() => {})">Try again</button>
            <div v-if="phone && form && passkeys" class="acct-pk">
              <PasskeySlide row :on="slideOn" :disabled="state === 'busy' || state === 'hello'" @toggle="slide" />
            </div>
          </div>
        </div>
      </section>

      <div v-if="!phone" class="win unit acct-blank" aria-hidden="true"><div class="unit-body"><span class="vents"></span></div></div>
    </main>
  </div>
</template>

<style scoped>
.rs.acct-desk { display: flex; flex-direction: column; min-height: 100dvh; }
.rs .acct-main { flex: 1; display: flex; flex-direction: column; justify-content: center; align-items: center; gap: 12px; padding: 0 var(--gutter) 40px; }
.rs .acct-blank, .rs .acct-unit.unit { width: min(780px, 100%); flex: none; }
.rs .acct-blank { height: 58px; }
.rs .acct-blank .unit-body { justify-content: center; padding: 0 18px; }
.rs .vents { display: block; width: min(420px, 100%); height: 16px; border-radius: 3px; background: repeating-linear-gradient(90deg, #0A0C10 0 4px, transparent 4px 14px); opacity: .75; }
.rs .acct-unit .unit-body.acct-body { flex-wrap: nowrap; align-items: stretch; gap: 22px 30px; padding: 22px 24px; }
.rs .acct-left { width: 214px; flex: none; display: flex; flex-direction: column; align-items: center; gap: 12px; }
.rs .acct-slide { margin-top: auto; align-items: center; padding-bottom: 6px; }
.rs .acct-right { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 16px; }
.rs .acct-lcd { flex: none; min-height: 86px; justify-content: center; }
.rs .acct-form { display: flex; flex-direction: column; gap: 14px; }
.rs .acct-form .lbl-f .input { text-transform: none; letter-spacing: normal; font-family: var(--font); font-weight: 400; }
.rs .acct-lamps { display: flex; align-items: center; justify-content: center; gap: 10px; }
.rs .acct-p { margin: 0; }
.rs .acct-term { margin: 0; padding: 10px 12px; overflow: hidden; background: #07090C; border: 1px solid var(--ink); border-radius: 5px; box-shadow: inset 1px 2px 6px rgba(0, 0, 0, .9); color: #CFE9C9; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; white-space: pre-wrap; }
.rs .acct-term .ts { color: #8F93A8; }
.rs .acct-pk { border-top: 1px solid var(--line); }
/* The slide's row is a switch, not a list row, so it takes no hover background. */
.rs .acct-pk .slide.row:hover { background: transparent; }
/* The phone centers the unit. While the keyboard is open it moves to the top, above the keys, so Sign in stays in view. */
.rs .acct-stack { top: 10px; justify-content: center; }
.rs .acct-stack[data-keyboard] { justify-content: flex-start; }
.rs .acct-stack > .acct-unit { flex: 0 1 auto; }
.rs.acct-phone .acct-body { gap: 14px; padding: 16px 14px; }
.rs.acct-phone .acct-lcd { min-height: 80px; }
</style>
