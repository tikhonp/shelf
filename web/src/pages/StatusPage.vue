<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Source, WrapperState } from '../api/types'
import Icon from '../components/Icon.vue'
import NoticeChoice from '../components/NoticeChoice.vue'
import NoticeSlot from '../components/NoticeSlot.vue'
import PartCard from '../components/PartCard.vue'
import SignIn from '../components/SignIn.vue'
import WrapperLog from '../components/WrapperLog.vue'
import { useNow } from '../lib/clock'
import { ago, count } from '../lib/format'
import { sourceLamp } from '../lib/lamps'
import { usePhone } from '../lib/viewport'
import { useSources } from '../stores/sources'
import { useStatus } from '../stores/status'
import { useTheme } from '../stores/theme'

const route = useRoute()
const router = useRouter()
const phone = usePhone()
const now = useNow()
const sources = useSources()
const status = useStatus()
const theme = useTheme()

onMounted(() => void status.check())

const signingIn = computed(() => route.query.signin === '1')
const openSignIn = () => void router.push({ query: { signin: '1' } })
const closeSignIn = () => void router.replace({ query: {} })

const stateWords: Record<WrapperState, string> = {
  starting: 'WRAPPER STARTING',
  running: 'WRAPPER RUNNING',
  signed_out: 'WRAPPER SIGNED OUT',
  signing_in: 'WRAPPER SIGNING IN',
  awaiting_code: 'WAITING FOR THE CODE',
  error: 'WRAPPER ERROR',
}

const apple = computed(() => sources.byId('applemusic'))
const qobuz = computed(() => sources.byId('qobuz'))
const others = computed(() => sources.list.filter((s) => s.id !== 'applemusic' && s.id !== 'qobuz'))

const appleLines = computed(() => {
  if (!apple.value?.enabled) return ['NOT SET UP']
  const w = status.wrapper
  if (!w) return [status.wrapperAnswers ? 'READING THE WRAPPER' : "WRAPPER DOESN'T ANSWER"]
  return [stateWords[w.state], ...(w.storefront ? [`STOREFRONT ${w.storefront.toUpperCase()}`] : [])]
})
const canSignIn = computed(() => {
  const s = status.wrapper?.state
  return !!apple.value?.enabled && s !== 'running'
})

interface QobuzDetail {
  label?: string
  proxy?: boolean
}
const qobuzDetail = computed(() => (qobuz.value?.health.detail ?? {}) as QobuzDetail)
const qobuzNote = computed(() => {
  const d = qobuzDetail.value
  if (!d.label) return qobuz.value?.enabled ? '' : 'No token'
  return `${d.label} subscription · proxy ${d.proxy ? 'on' : 'off'}`
})
const qobuzLines = computed(() => {
  const q = qobuz.value
  if (!q?.enabled) return ['NOT SET UP']
  return [q.health.ok ? 'TOKEN WORKS' : (q.health.message || 'TOKEN FAILED').toUpperCase()]
})

const navidromeLines = computed(() => {
  const s = status.scan
  if (!s?.enabled) return ['NOT SET UP']
  if (!s.ok) return [(s.message || "DOESN'T ANSWER").toUpperCase()]
  const scan = s.scanning ? `SCANNING · ${s.count}` : s.last_scan ? `LAST SCAN ${ago(s.last_scan, now.value).toUpperCase()}` : 'NO SCAN YET'
  return [`ONLINE · ${s.latency_ms} MS`, scan]
})
const navidromeLamp = computed(() => (!status.scan?.enabled ? 'off' : status.scan.ok ? 'ok' : 'bad'))

const telegramLines = computed(() => {
  const t = status.telegram
  if (!t?.enabled) return ['NOT SET UP']
  return [t.connected ? 'CONNECTED' : 'NOT CONNECTED', `ANSWERS ${count(t.users, 'USER')}`]
})
const telegramLamp = computed(() => (!status.telegram?.enabled ? 'off' : status.telegram.connected ? 'ok' : 'bad'))

const lines = (s: Source) => [s.enabled ? (s.health.ok ? 'WORKS' : (s.health.message ?? 'FAILED').toUpperCase()) : 'NOT SET UP']
</script>

<template>
  <SignIn v-if="signingIn && phone" @close="closeSignIn" />
  <div v-else-if="phone" class="stack">
    <section class="win" aria-label="Status">
      <div class="tb">
        <h1>Status</h1>
        <span class="stripes"></span>
        <button type="button" class="wbtn" aria-label="Check again" title="Check again" @click="status.check()"><Icon name="scan" /></button>
      </div>
      <div class="vscroll">
        <PartCard v-if="apple" name="Apple Music" :lamp="sourceLamp(apple)" note="The wrapper" :lines="appleLines">
          <template v-if="canSignIn" #text><p class="t2">{{ apple.health.message }}</p></template>
          <template v-if="apple.enabled" #keys>
            <button v-if="canSignIn" type="button" class="key sun" @click="openSignIn"><Icon name="user" />Sign in</button>
            <button type="button" class="key" @click="status.restart().then(status.applyWrapper)">Restart</button>
          </template>
          <WrapperLog v-if="apple.enabled" />
        </PartCard>
        <PartCard v-if="qobuz" name="Qobuz" :lamp="sourceLamp(qobuz)" :note="qobuzNote" :lines="qobuzLines">
          <template v-if="!qobuz.health.ok" #text>
            <ol class="steps">
              <li>Sign in at play.qobuz.com in a browser.</li>
              <li>Copy the user auth token from the web player's local storage.</li>
              <li>Put it in <code class="mono">SHELF_QOBUZ_TOKEN</code> and restart shelf.</li>
            </ol>
          </template>
        </PartCard>
        <PartCard v-for="s in others" :key="s.id" :name="s.name" :lamp="sourceLamp(s)" :lines="lines(s)" />
        <PartCard name="Navidrome" :lamp="navidromeLamp" :note="status.scan?.address" :lines="navidromeLines">
          <template v-if="status.scan?.enabled" #keys><button type="button" class="key" :disabled="status.scan.scanning" @click="status.startScan()">Scan now</button></template>
        </PartCard>
        <PartCard name="Telegram" :lamp="telegramLamp" :note="status.telegram?.username ? `@${status.telegram.username}` : ''" :lines="telegramLines" />
        <NoticeChoice />
        <section class="smod last" aria-label="Theme">
          <div class="smod-h"><h2>Theme</h2></div>
          <div class="segk block" role="radiogroup" aria-label="Theme">
            <button type="button" role="radio" :aria-checked="!theme.day" @click="theme.day = false">Night</button>
            <button type="button" role="radio" :aria-checked="theme.day" @click="theme.day = true">Day</button>
          </div>
        </section>
      </div>
    </section>
    <NoticeSlot />
  </div>
  <div v-else class="col g16 desk-col">
    <NoticeSlot />
    <PartCard v-if="qobuz" name="Qobuz" :lamp="sourceLamp(qobuz)" :note="qobuzNote" :lines="qobuzLines">
      <template v-if="!qobuz.health.ok" #text>
        <div class="note replace">
          <div class="col g4">
            <strong class="px small-h">Replace the token</strong>
            <ol class="sub steps">
              <li>Sign in at play.qobuz.com in a browser.</li>
              <li>Copy the user auth token from the web player's local storage.</li>
              <li>Put it in <code class="mono">SHELF_QOBUZ_TOKEN</code> and restart shelf.</li>
            </ol>
          </div>
        </div>
      </template>
    </PartCard>
    <PartCard v-if="apple" name="Apple Music" :lamp="sourceLamp(apple)" note="The wrapper, for decryption" :lines="appleLines">
      <template v-if="apple.enabled" #keys>
        <button v-if="canSignIn" type="button" class="key sun" @click="openSignIn"><Icon name="user" />Sign in</button>
        <button type="button" class="key" @click="status.restart().then(status.applyWrapper)">Restart</button>
      </template>
      <WrapperLog v-if="apple.enabled" />
    </PartCard>
    <PartCard v-for="s in others" :key="s.id" :name="s.name" :lamp="sourceLamp(s)" :lines="lines(s)" />
    <div class="pair">
      <PartCard name="Navidrome" :lamp="navidromeLamp" :note="status.scan?.address" :lines="navidromeLines">
        <template v-if="status.scan?.enabled" #keys><button type="button" class="key" :disabled="status.scan.scanning" @click="status.startScan()">Scan now</button></template>
      </PartCard>
      <PartCard name="Telegram" :lamp="telegramLamp" :note="status.telegram?.username ? `@${status.telegram.username}` : ''" :lines="telegramLines" />
    </div>
    <NoticeChoice />
    <SignIn v-if="signingIn" @close="closeSignIn" />
  </div>
</template>

<style scoped>
.rs .desk-col { flex: 1; max-width: 1180px; padding: 20px var(--gutter) 28px; }
.rs .pair { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: 16px; }
.rs .steps { display: flex; flex-direction: column; gap: 4px; margin: 0; padding-left: 20px; color: var(--text-2); }
.rs code { color: var(--text); }
.rs .replace { flex: 1 1 360px; border-color: var(--red); }
</style>
