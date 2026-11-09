<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import type { Op, ProposalSummary } from '../api/agents'
import { useNow } from '../lib/clock'
import { finishedAt } from '../lib/format'
import { typeAngles } from '../lib/formats'
import { opLabel, stateClass, stateWord, summaryText } from '../lib/inbox'
import { usePhone } from '../lib/viewport'
import { useAgents } from '../stores/agents'
import { filtersToQuery, useInbox, type Filters, type StateFilter } from '../stores/inbox'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import Knob from './Knob.vue'
import PickMenu from './PickMenu.vue'

defineProps<{ selected?: number }>()
const inbox = useInbox()
const agents = useAgents()
const route = useRoute()
const router = useRouter()
const now = useNow()
const phone = usePhone()

const states: { id: StateFilter; label: string }[] = [
  { id: 'pending', label: 'Pending' },
  { id: 'reviewed', label: 'Reviewed' },
  { id: 'withdrawn', label: 'Withdrawn' },
  { id: 'all', label: 'All' },
]
const at = computed(() => states.findIndex((s) => s.id === inbox.filters.state))
const turn = () => set({ state: states[(at.value + 1) % states.length]!.id })
const kinds: { id: Filters['kind']; label: string }[] = [
  { id: '', label: 'Tags and covers' },
  { id: 'tag', label: 'Tags' },
  { id: 'cover', label: 'Covers' },
]
const agentOptions = computed(() => [{ id: 0, label: 'Every agent' }, ...agents.connections.map((c) => ({ id: c.id, label: c.revoked_at ? `${c.name} (revoked)` : c.name }))])

// Filters change the address, and the page reads them back from it.
function set(over: Partial<Filters>) {
  void router.push({ path: route.path, query: filtersToQuery({ ...inbox.filters, ...over }) })
}
const query = computed(() => filtersToQuery(inbox.filters))
const albumName = computed(() => {
  const a = inbox.proposals.find((p) => p.album.id === inbox.filters.album || p.album.current_id === inbox.filters.album)?.album
  return a ? `${a.title} · ${a.artist}` : `Album ${inbox.filters.album}`
})
const cover = (p: ProposalSummary) => (p.album.current_id ? `/api/library/albums/${p.album.current_id}/cover` : '')
// The list sends each op without its values, which is all opLabel reads.
const fieldNames = (p: ProposalSummary) => [...new Set((p.fields ?? []).map((f) => opLabel(f as Op)))].join(', ') || summaryText(p.summary)
const second = (p: ProposalSummary) => [p.album.artist, fieldNames(p)].filter(Boolean).join(' · ')
// A row names its state only when the filter doesn't already say it.
const pill = (p: ProposalSummary) => inbox.filters.state === 'all' || (p.state === 'pending' && p.changes_requested)
</script>

<template>
  <section class="win" aria-labelledby="ib-h">
    <div class="tb">
      <RouterLink v-if="phone" class="wbtn" to="/library" aria-label="Back to the library" title="Library"><Icon name="back" /></RouterLink>
      <h1 id="ib-h">Inbox</h1>
      <span class="stripes"></span>
      <RouterLink class="wbtn txt" to="/inbox/memory">Memory</RouterLink>
    </div>
    <div class="rotary" role="radiogroup" aria-label="State">
      <div class="rot-labels">
        <button v-for="s in states.slice(0, 2)" :key="s.id" type="button" role="radio" :aria-checked="s.id === inbox.filters.state" @click="set({ state: s.id })"><span class="led"></span>{{ s.label }}</button>
        <Knob color="grey" :size="phone ? 48 : 44" :angle="typeAngles[at] ?? 0" :label="`Change the state, now ${states[at]?.label}`" @press="turn" />
        <button v-for="s in states.slice(2)" :key="s.id" type="button" role="radio" :aria-checked="s.id === inbox.filters.state" @click="set({ state: s.id })"><span class="led"></span>{{ s.label }}</button>
      </div>
    </div>
    <div class="ib-filters">
      <PickMenu label="Agent" :options="agentOptions" :value="inbox.filters.connection ?? 0" @pick="(id) => set({ connection: id || null })" />
      <PickMenu label="Kind" :options="kinds" :value="inbox.filters.kind" @pick="(id) => set({ kind: id })" />
      <span v-if="inbox.filters.album" class="ib-chip"><span class="ell">{{ albumName }}</span><button type="button" class="link" aria-label="Show every album" @click="set({ album: null })">Every album</button></span>
    </div>
    <p v-if="inbox.listError" class="bad-text pad" role="alert">{{ inbox.listError }}</p>
    <div v-if="!inbox.listLoading && inbox.proposals.length === 0 && !inbox.listError" class="empty">
      <h2>{{ inbox.filters.state === 'pending' ? 'Nothing to review' : 'No proposals' }}</h2>
      <p>Proposals from a connected agent show here. Status has the setup prompt.</p>
    </div>
    <ul v-else class="rows ib-rows" aria-label="Proposals">
      <li v-for="p in inbox.proposals" :key="p.id" class="row ib-row" :class="{ cur: selected === p.id }">
        <Cover :url="cover(p)" :size="46" />
        <RouterLink class="two plain" :to="{ path: `/inbox/${p.id}`, query }" :aria-current="selected === p.id ? 'page' : undefined">
          <span class="ttl"><span class="ell">{{ p.album.title }}</span></span>
          <span class="sub ell">{{ second(p) }}</span>
          <span class="sub ell t3">{{ p.connection.name }} · {{ finishedAt(p.updated_at, now) }}<template v-if="p.stale"> · Changed since proposed</template></span>
        </RouterLink>
        <span class="col g4 r">
          <span v-if="p.confidence != null" class="lcd ib-sure num" :aria-label="`Confidence ${p.confidence}%`">{{ p.confidence }}%</span>
          <span v-if="pill(p)" class="pill" :class="stateClass(p.state, p.changes_requested)">{{ stateWord(p) }}</span>
        </span>
      </li>
    </ul>
    <div v-if="inbox.next !== null" class="pad"><button type="button" class="key block" @click="inbox.moreList()">Load more</button></div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 14px; }
.rs .ib-filters { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 10px 14px; border-bottom: 1px solid var(--ink); }
.rs .ib-rows { flex: 1; min-height: 0; margin: 0; padding: 0; list-style: none; overflow-y: auto; }
.rs .ib-row { grid-template-columns: 46px minmax(0, 1fr) auto; }
.rs .ib-row .two { gap: 2px; }
.rs .ib-row .two::after { content: ""; position: absolute; inset: 0; }
.rs .ib-row { position: relative; }
.rs .ib-sure { flex: none; display: inline-flex; align-items: center; justify-content: center; min-width: 48px; height: 24px; padding: 0 6px; font: 400 15px/1 var(--disp); }
.rs .ib-chip { display: inline-flex; align-items: center; gap: 10px; max-width: 100%; min-height: 32px; padding: 0 10px; background: var(--win-2); border: 1px solid var(--ink); border-radius: 4px; font-size: 13px; }
.rs.day .rot-labels .led { --c: #9AA0A9; }
</style>
