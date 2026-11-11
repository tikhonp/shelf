<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useNow } from '../lib/clock'
import { ago, clock } from '../lib/format'
import { sourceLamp } from '../lib/lamps'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useBulk } from '../stores/bulk'
import { useHelp } from '../stores/help'
import { useInbox } from '../stores/inbox'
import { useEvents } from '../stores/events'
import { useSources } from '../stores/sources'
import { useSettings } from '../stores/settings'
import { useStatus } from '../stores/status'
import { useTheme } from '../stores/theme'
import Icon, { type IconName } from './Icon.vue'

const route = useRoute()
const phone = usePhone()
const now = useNow()
const sources = useSources()
const downloads = useDownloads()
const status = useStatus()
const events = useEvents()
const theme = useTheme()
const settings = useSettings()
const help = useHelp()
const bulk = useBulk()
const inbox = useInbox()

// Inbox shows only while the library agent is on, which GET /api/inbox answering says. The phone keeps four tabs and opens the Inbox from the Library's title bar.
const tabs = computed<{ to: string; label: string; icon: IconName }[]>(() => [
  { to: '/library', label: 'Library', icon: 'library' },
  ...(inbox.enabled && !phone.value ? [{ to: '/inbox', label: 'Inbox', icon: 'inbox' as IconName }] : []),
  { to: '/search', label: 'Search', icon: 'search' },
  { to: '/downloads', label: 'Downloads', icon: 'download' },
  { to: '/status', label: 'Status', icon: 'status' },
])
// On the phone the Inbox opens from the Library, so the Library tab stays lit there.
const isOn = (to: string) => route.path.startsWith(to) || (to === '/library' && phone.value && route.path.startsWith('/inbox'))
// Memory is one long page, so only the Inbox's list and proposal fit the window.
const fits = computed(() => isOn('/library') || route.name === 'inbox' || route.name === 'proposal')

// lampFor is what a tab's amber lamp says, or nothing when the tab has no lamp.
function lampFor(to: string): string {
  if (to === '/downloads') return downloads.busy ? 'downloads in progress' : ''
  if (to === '/status') return status.attention.join(', ')
  const waiting = inbox.info?.pending ? `${inbox.info.pending} ${inbox.info.pending === 1 ? 'proposal' : 'proposals'} to review` : ''
  if (to === '/library') return [bulk.working ? 'bulk run in progress' : '', phone.value ? waiting : ''].filter(Boolean).join(', ')
  if (to === '/inbox') return waiting
  return ''
}

const section = computed(() => tabs.value.find((t) => isOn(t.to))?.to)
const current = computed(() => downloads.running[0])
const two = (n: number) => String(n).padStart(2, '0')
const lastScan = computed(() => {
  const s = status.scan
  if (!s?.enabled) return ''
  if (s.scanning) return 'SCANNING'
  return s.last_scan ? `LAST SCAN ${ago(s.last_scan, now.value).toUpperCase()}` : 'NEVER SCANNED'
})
const checked = computed(() => (status.checkedAt ? `Checked ${Math.max(0, Math.round((now.value - status.checkedAt) / 1000))} s ago` : ''))

const lamps = computed(() => [
  ...sources.list.map((s) => ({ id: s.id, cls: sourceLamp(s), title: s.health.ok ? `${s.name} works` : s.health.message || s.name })),
  ...(status.scan?.enabled
    ? [{ id: 'navidrome', cls: status.scan.ok ? 'ok' : 'bad', title: status.scan.ok ? 'Navidrome answers' : "Navidrome doesn't answer" }]
    : []),
])
const lampWords = computed(() => lamps.value.map((l) => l.title).join(', '))

onMounted(() => {
  events.connect()
  void Promise.allSettled([downloads.load(), status.check(), bulk.check(), inbox.detect()])
})
</script>

<template>
  <div class="rs desk" :class="{ day: theme.day, console: !phone, compact: !phone && settings.density === 'compact', 'shell-phone': phone, 'shell-desk': !phone, 'shell-fit': !phone && fits }">
    <header v-if="!phone" class="menubar">
      <RouterLink class="brand" to="/"><span class="logo" aria-hidden="true"><i></i><i></i><i></i></span>Music Shelf</RouterLink>
      <nav class="tabs" aria-label="Main">
        <RouterLink v-for="t in tabs" :key="t.to" class="mb-item" :class="{ on: isOn(t.to) }" :to="t.to" :aria-current="isOn(t.to) ? 'page' : undefined">
          {{ t.label }}<template v-if="lampFor(t.to)"><span class="led" :title="lampFor(t.to)"></span><span class="sr">, {{ lampFor(t.to) }}</span></template>
        </RouterLink>
      </nav>
      <div class="mb-right">
        <RouterLink v-if="section === '/search' && current" class="mb-item now" to="/downloads"><span class="led rec blink"></span><span class="num">{{ two(current.tracks_done) }}/{{ two(current.tracks_total) }}</span> <span class="ell">{{ current.title }}</span></RouterLink>
        <template v-if="section === '/downloads' && lastScan">
          <span class="vfd small">{{ lastScan }}</span>
          <button type="button" class="key sm" :disabled="status.scan?.scanning" @click="status.startScan()"><Icon name="scan" size="s" />Scan now</button>
        </template>
        <template v-if="section === '/status'">
          <span class="checked sm">{{ checked }}</span>
          <button type="button" class="key sm" @click="status.check()">Check again</button>
        </template>
        <span class="leds" role="img" :aria-label="lampWords"><span v-for="l in lamps" :key="l.id" class="led" :class="l.cls" :title="l.title"></span></span>
        <button v-if="section === '/library'" type="button" class="key sm icon" aria-label="Help" title="Help" @click="help.sheet = 'guide'">?</button>
        <button type="button" class="key sm icon" :aria-label="theme.day ? 'Switch to Night' : 'Switch to Day'" :title="theme.day ? 'Switch to Night' : 'Switch to Day'" @click="theme.day = !theme.day"><Icon :name="theme.day ? 'moon' : 'sun'" /></button>
        <span class="vfd num">{{ clock(now) }}</span>
      </div>
    </header>
    <slot />
    <nav v-if="phone" class="dock" aria-label="Main">
      <RouterLink v-for="t in tabs" :key="t.to" :class="{ on: isOn(t.to) }" :to="t.to" :aria-current="isOn(t.to) ? 'page' : undefined">
        <Icon :name="t.icon" />{{ t.label }}<template v-if="lampFor(t.to)"><span class="led" :title="lampFor(t.to)"></span><span class="sr">, {{ lampFor(t.to) }}</span></template>
      </RouterLink>
    </nav>
  </div>
</template>

<style scoped>
.rs .checked { color: #C9CCD2; white-space: nowrap; }
.rs .now { max-width: 320px; font-size: 15px; }
@media (max-width: 999px) {
  .rs .checked, .rs .now { display: none; }
}
/* The Library's and the Inbox's panels scroll by themselves, so the page fits the window. */
.rs.shell-fit { display: flex; flex-direction: column; height: 100dvh; overflow: hidden; }
</style>
