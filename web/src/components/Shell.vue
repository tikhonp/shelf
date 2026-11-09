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
  <div class="rs desk" :class="{ day: theme.day, console: !phone, compact: !phone && settings.density === 'compact', 'shell-phone': phone, 'shell-desk': !phone, 'shell-fit': !phone && isOn('/library') }">
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
.rs .menubar { position: relative; z-index: 5; display: flex; align-items: center; gap: 6px; min-height: 52px; padding: 0 var(--gutter); background: linear-gradient(180deg, #1E232B, #12151A); border-bottom: 1px solid var(--ink); box-shadow: inset 0 1px 0 rgba(255, 255, 255, .06), 0 3px 10px rgba(0, 0, 0, .35); color: #ECE8DF; }
.rs .brand { display: flex; align-items: center; gap: 10px; margin-right: 14px; font-family: var(--label); font-size: 16px; font-weight: 700; letter-spacing: .14em; text-transform: uppercase; white-space: nowrap; color: #ECE8DF; text-decoration: none; }
.rs .logo { display: flex; align-items: flex-end; gap: 2px; flex: none; height: 20px; padding: 2px 3px; background: #07090C; border-radius: 3px; box-shadow: inset 0 1px 2px rgba(0, 0, 0, .8); }
.rs .logo i { display: block; width: 4px; border-radius: 1px; }
.rs .logo i:nth-child(1) { height: 8px; background: var(--green); box-shadow: 0 0 6px var(--green); }
.rs .logo i:nth-child(2) { height: 12px; background: var(--yellow); box-shadow: 0 0 6px var(--yellow); }
.rs .logo i:nth-child(3) { height: 15px; background: var(--red); box-shadow: 0 0 6px var(--red); }
.rs .mb-item { position: relative; display: inline-flex; align-items: center; gap: 8px; height: 34px; padding: 0 14px; border: 1px solid #07090C; border-radius: 5px; background: linear-gradient(180deg, #4A5262, #3A4150); box-shadow: inset 0 1px 0 rgba(255, 255, 255, .2), 0 2px 0 #07090C; font-family: var(--label); font-size: 14px; font-weight: 600; letter-spacing: .08em; text-transform: uppercase; color: #ECE8DF; text-decoration: none; white-space: nowrap; }
.rs .mb-item:hover { filter: brightness(1.1); }
.rs .mb-item.on { background: radial-gradient(ellipse at 50% 40%, var(--lit-hi), var(--lamp) 55%, var(--lit-lo)); color: var(--lit-text); box-shadow: inset 0 1px 0 #fff, 0 1px 0 #07090C, 0 0 calc(12px * var(--glow)) rgba(244, 238, 220, .45); transform: translateY(1px); }
.rs .mb-right { margin-left: auto; display: flex; align-items: center; gap: 16px; }
.rs .vfd { display: inline-flex; align-items: center; height: 32px; padding: 0 10px; background: var(--lcd); border-radius: 4px; box-shadow: inset 1px 2px 4px rgba(0, 0, 0, .8); color: var(--lcd-text); font: 400 18px/1 var(--disp); text-shadow: 0 0 6px var(--lcd-glow); }
.rs .dock a .led { --c: var(--sun); position: absolute; top: 7px; right: 7px; }
.rs .menubar nav .mb-item .led { --c: var(--sun); position: absolute; top: 4px; right: 4px; width: 9px; height: 9px; }
.rs .dock { position: absolute; left: 8px; right: 12px; bottom: 14px; z-index: 30; display: flex; gap: 6px; padding: 7px; background: linear-gradient(180deg, rgba(255, 255, 255, .14), rgba(0, 0, 0, .28)), repeating-linear-gradient(1deg, #4E2B18 0 2px, #5E351F 2px 5px, #53301B 5px 8px, #6A3D24 8px 12px, #573220 12px 15px); border: 1px solid var(--ink); border-radius: 12px; box-shadow: inset 1px 1px 0 rgba(255, 255, 255, .08), 4px 8px 20px rgba(0, 0, 0, .5); }
.rs .dock a { position: relative; flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px; min-height: 56px; border: 1px solid #07090C; border-radius: 4px; background: linear-gradient(180deg, var(--key-hi), var(--key) 60%, var(--key-lo)); box-shadow: inset 0 1px 0 var(--key-hl), inset 0 -2px 0 rgba(0, 0, 0, .1), 0 3px 0 #07090C; font-family: var(--label); font-size: 12px; font-weight: 700; letter-spacing: .05em; text-transform: uppercase; color: var(--key-text); text-decoration: none; }
.rs .dock a.on { background: radial-gradient(ellipse at 50% 40%, var(--lit-hi), var(--lamp) 55%, var(--lit-lo)); color: var(--lit-text); box-shadow: inset 0 1px 0 #fff, inset 0 0 10px rgba(240, 150, 60, .35), 0 0 calc(12px * var(--glow)) var(--lit-glow); transform: translateY(3px); }
.rs .menubar { flex-wrap: wrap; padding-top: 6px; padding-bottom: 6px; row-gap: 6px; }
.rs .tabs { display: flex; gap: 6px; }
.rs .vfd.small { font-size: 15px; }
.rs .checked { color: #C9CCD2; white-space: nowrap; }
.rs .now { max-width: 320px; font-size: 15px; }
@media (max-width: 999px) {
  .rs .checked, .rs .now { display: none; }
}
.rs .dock { bottom: calc(14px + env(safe-area-inset-bottom)); }
/* Library's panels scroll by themselves, so the page fits the window. */
.rs.shell-fit { display: flex; flex-direction: column; height: 100dvh; overflow: hidden; }
</style>
