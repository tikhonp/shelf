<script setup lang="ts">
import { computed, nextTick, onScopeDispose, ref } from 'vue'
import { useRoute } from 'vue-router'
import { display, failed, reducedMotion, type SignState } from '../lib/account'
import { useNow } from '../lib/clock'
import { clock } from '../lib/format'
import { passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { usePhone } from '../lib/viewport'
import { useAuth, type AuthStatus } from '../stores/auth'
import { inboxWasOn } from '../stores/inbox'
import { useTheme } from '../stores/theme'
import Icon, { type IconName } from './Icon.vue'
import KeySwitch from './KeySwitch.vue'

// The switched-off console before the app mounts. It has the menubar with dark tabs and the sign-in rack unit.
// While Shelf checks access, the unit shows only its display.
const auth = useAuth()
const theme = useTheme()
const phone = usePhone()
const route = useRoute()
const now = useNow()
const passkeys = supportsPasskeys()

const username = ref('')
const password = ref('')
const passwordEl = ref<HTMLInputElement>()
const state = ref<SignState>('out')
// on is where the key switch points. It turns to ON when a passkey signs in.
const on = ref(false)
// lit counts the menubar lamps the power-up has lit.
const lit = ref(0)

const tabs = computed<{ to: string; label: string; icon: IconName; opening: string }[]>(() => [
  { to: '/library', label: 'Library', icon: 'library', opening: 'THE LIBRARY' },
  ...(inboxWasOn() && !phone.value ? [{ to: '/inbox', label: 'Inbox', icon: 'inbox' as IconName, opening: 'THE INBOX' }] : []),
  { to: '/search', label: 'Search', icon: 'search', opening: 'SEARCH' },
  { to: '/downloads', label: 'Downloads', icon: 'download', opening: 'DOWNLOADS' },
  { to: '/status', label: 'Status', icon: 'status', opening: 'STATUS' },
])
// opening is the tab the app opens at, which lights during the power-up.
const opening = computed(() => tabs.value.find((t) => route.path.startsWith(t.to)) ?? tabs.value[0]!)

const phase = computed<SignState>(() => {
  if (!auth.ready) return auth.error ? 'noanswer' : 'checking'
  if (auth.setupNeeded) return 'setup'
  return state.value
})
const shown = computed(() => {
  const d = display(phase.value, { name: username.value, passkeys })
  if (phase.value === 'hello') d.lines[1] = `OPENING ${opening.value.opening}`
  return d
})
const form = computed(() => auth.ready && !auth.setupNeeded)
const busy = computed(() => state.value === 'busy' || state.value === 'waiting' || state.value === 'hello')

const timers: ReturnType<typeof setTimeout>[] = []
const wait = (ms: number) => new Promise<void>((resolve) => timers.push(setTimeout(resolve, ms)))
onScopeDispose(() => timers.forEach(clearTimeout))

// powerUp lights the menubar lamps 260ms apart and starts the clock, then mounts the app.
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
  try {
    const v = passkey ? await passkeyLogin(false) : await auth.login(username.value, password.value, false)
    if (!passkey) password.value = ''
    if (passkey && v.account) username.value = v.account.username
    await powerUp(v, passkey)
  } catch (e) {
    state.value = failed(e)
    on.value = false
    if (!passkey) {
      password.value = ''
      if (state.value === 'wrong') void nextTick(() => passwordEl.value?.focus())
    }
  }
}
const reload = () => window.location.reload()
</script>

<template>
  <div class="rs desk acct-desk" :class="{ day: theme.day, console: !phone, 'acct-phone': phone }">
    <header v-if="!phone" class="menubar acct-bar">
      <span class="brand"><span class="logo" :class="{ off: lit === 0 }" aria-hidden="true"><i></i><i></i><i></i></span>Music Shelf</span>
      <nav class="tabs acct-tabs" aria-label="Main, after sign-in">
        <span v-for="t in tabs" :key="t.to" class="mb-item" :class="{ on: phase === 'hello' && t.to === opening.to }" aria-disabled="true">{{ t.label }}</span>
      </nav>
      <div class="mb-right">
        <span class="leds" aria-hidden="true"><span v-for="i in 3" :key="i" class="led" :class="lit >= i ? 'ok' : 'off'"></span></span>
        <span class="vfd num acct-clock" :class="{ live: phase === 'hello' }" aria-label="Clock">{{ phase === 'hello' ? clock(now) : '--:--' }}</span>
      </div>
    </header>

    <main class="acct-main" :class="{ stack: phone }">
      <div v-if="phone" class="acct-top">
        <span class="brand"><span class="logo" :class="{ off: lit === 0 }" aria-hidden="true"><i></i><i></i><i></i></span>Music Shelf</span>
        <span class="acct-top-r" aria-hidden="true"><span v-for="i in 3" :key="i" class="led" :class="lit >= i ? 'ok' : 'off'"></span><span class="vfd num">{{ phase === 'hello' ? clock(now) : '--:--' }}</span></span>
      </div>
      <div v-if="!phone" class="win unit acct-blank" aria-hidden="true"><div class="unit-body"><span class="vents"></span></div></div>

      <section class="win acct-unit" :class="{ unit: !phone }" aria-labelledby="acct-h">
        <div v-if="phone" class="tb"><h1 id="acct-h"><span class="led" :class="shown.lamp"></span>Sign in</h1><span class="stripes"></span></div>
        <div :class="phone ? 'vscroll acct-body' : 'unit-body acct-body'">
          <div v-if="!phone" class="acct-left">
            <div class="acct-plate">
              <h1 id="acct-h"><span class="led" :class="shown.lamp"></span>Sign in</h1>
              <span class="sub">to Music Shelf</span>
            </div>
            <template v-if="form && passkeys">
              <KeySwitch :on="on" />
              <button class="key block" type="button" :class="{ pressed: state === 'waiting' }" :disabled="busy" @click="signIn(true)">Sign in with a passkey</button>
            </template>
          </div>

          <div class="acct-right">
            <div class="lcd acct-lcd" role="status"><div v-for="(l, i) in shown.lines" :key="i" class="lcd-l">{{ l }}</div></div>
            <form v-if="form" class="acct-form" @submit.prevent="signIn()">
              <label class="lbl-f">Username<input v-model="username" class="input" name="username" autocomplete="username webauthn" autocapitalize="none" spellcheck="false" required maxlength="32" autofocus :readonly="busy" /></label>
              <label class="lbl-f">Password<input ref="passwordEl" v-model="password" class="input" name="password" type="password" autocomplete="current-password" required :readonly="busy" /></label>
              <button class="key sun lg block" type="submit" :class="{ pressed: state === 'busy' }" :disabled="busy">Sign in</button>
            </form>
            <template v-else-if="phase === 'setup'">
              <p class="sm acct-p">Run this in the server's terminal, then reload the page.</p>
              <pre class="acct-term"><span class="ts">$</span> shelf manage users add &lt;username&gt;</pre>
              <button class="key block" type="button" @click="reload">Reload</button>
            </template>
            <button v-else-if="phase === 'noanswer'" class="key sun block" type="button" @click="auth.check().catch(() => {})">Try again</button>
            <div v-if="phone && form && passkeys" class="acct-pk">
              <KeySwitch :on="on" :lamp="false" :width="96" />
              <button class="key block" type="button" :class="{ pressed: state === 'waiting' }" :disabled="busy" @click="signIn(true)">Sign in with a passkey</button>
            </div>
          </div>
        </div>
      </section>

      <div v-if="!phone" class="win unit acct-blank" aria-hidden="true"><div class="unit-body"><span class="vents"></span></div></div>
    </main>

    <nav v-if="phone" class="dock acct-dock" aria-hidden="true">
      <a v-for="t in tabs" :key="t.to" :class="{ on: phase === 'hello' && t.to === opening.to }"><Icon :name="t.icon" />{{ t.label }}</a>
    </nav>
  </div>
</template>

<style scoped>
.rs.acct-desk { display: flex; flex-direction: column; min-height: 100dvh; }
.rs .acct-tabs { opacity: .35; transition: opacity .4s; }
.rs .acct-tabs .mb-item.on { opacity: 1; }
.rs .acct-bar .leds { display: flex; align-items: center; gap: 10px; }
.rs .acct-clock { opacity: .5; }
.rs .acct-clock.live { opacity: 1; }
.rs .acct-main { flex: 1; display: flex; flex-direction: column; justify-content: center; align-items: center; gap: 12px; padding: 0 var(--gutter) 40px; }
.rs .acct-blank, .rs .acct-unit.unit { width: min(780px, 100%); flex: none; }
.rs .acct-blank { height: 58px; }
.rs .acct-blank .unit-body { justify-content: center; padding: 0 18px; }
.rs .vents { display: block; width: min(420px, 100%); height: 16px; border-radius: 3px; background: repeating-linear-gradient(90deg, #0A0C10 0 4px, transparent 4px 14px); opacity: .75; }
.rs .acct-unit .unit-body.acct-body { flex-wrap: nowrap; align-items: stretch; gap: 22px 30px; padding: 22px 24px; }
.rs .acct-left { width: 214px; flex: none; display: flex; flex-direction: column; align-items: center; gap: 12px; }
.rs .acct-plate { align-self: stretch; display: flex; flex-direction: column; gap: 4px; }
.rs .acct-plate h1 { display: flex; align-items: center; gap: 10px; margin: 0; font-family: var(--label); font-size: 18px; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
.rs .acct-left .keysw { margin-top: 4px; }
.rs .acct-right { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 16px; }
.rs .acct-lcd { flex: none; min-height: 66px; justify-content: center; }
.rs .acct-form { display: flex; flex-direction: column; gap: 14px; }
.rs .acct-form .lbl-f .input { text-transform: none; letter-spacing: normal; font-family: var(--font); font-weight: 400; }
.rs .acct-p { margin: 0; }
.rs .acct-term { margin: 0; padding: 10px 12px; overflow: hidden; background: #07090C; border: 1px solid var(--ink); border-radius: 5px; box-shadow: inset 1px 2px 6px rgba(0, 0, 0, .9); color: #CFE9C9; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; white-space: pre-wrap; }
.rs .acct-term .ts { color: #8F93A8; }
.rs .acct-pk { display: flex; align-items: center; gap: 14px; padding-top: 6px; border-top: 1px solid var(--line); }
.rs .acct-pk .key { flex: 1; }
.rs .acct-top { flex: none; display: flex; align-items: center; justify-content: space-between; height: 40px; padding: 0 6px; }
.rs .acct-top .brand { margin-right: 0; color: var(--text); }
.rs .acct-top-r { display: flex; align-items: center; gap: 8px; }
.rs.acct-phone .acct-main { padding: 0; }
.rs.acct-phone .acct-main.stack { position: absolute; justify-content: flex-start; align-items: stretch; }
/* The unit is a window at the top of the stack, so the keyboard never covers Sign in. */
.rs.acct-phone .acct-unit { flex: 0 1 auto; }
.rs.acct-phone .acct-body { gap: 14px; padding: 16px 14px; }
.rs.acct-phone .tb h1 { display: flex; align-items: center; gap: 8px; }
.rs .acct-dock { opacity: .35; }
</style>
