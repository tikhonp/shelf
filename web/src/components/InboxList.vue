<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import type { ProposalSummary } from '../api/agents'
import { useNow } from '../lib/clock'
import { finishedAt } from '../lib/format'
import { stateClass, stateWord, summaryText } from '../lib/inbox'
import { useAgents } from '../stores/agents'
import { filtersToQuery, useInbox, type Filters, type StateFilter } from '../stores/inbox'

defineProps<{ selected?: number }>()
const inbox = useInbox()
const agents = useAgents()
const route = useRoute()
const router = useRouter()
const now = useNow()

const states: { id: StateFilter; label: string }[] = [
  { id: 'pending', label: 'Pending' },
  { id: 'reviewed', label: 'Reviewed' },
  { id: 'withdrawn', label: 'Withdrawn' },
  { id: 'all', label: 'All' },
]

// Filters change the address, and the page reads them back from it.
function set(over: Partial<Filters>) {
  void router.push({ path: route.path, query: filtersToQuery({ ...inbox.filters, ...over }) })
}
const query = computed(() => filtersToQuery(inbox.filters))
const albumName = computed(() => {
  const a = inbox.proposals.find((p) => p.album.id === inbox.filters.album || p.album.current_id === inbox.filters.album)?.album
  return a ? `${a.title} · ${a.artist}` : `Album ${inbox.filters.album}`
})
const agentOptions = computed(() => agents.connections.map((c) => ({ id: c.id, name: c.revoked_at ? `${c.name} (revoked)` : c.name })))
const sub = (p: ProposalSummary) => [p.album.title, p.album.artist, p.connection.name].filter(Boolean).join(' · ')
const onAgent = (e: Event) => set({ connection: Number((e.target as HTMLSelectElement).value) || null })
const onKind = (e: Event) => set({ kind: (e.target as HTMLSelectElement).value as Filters['kind'] })
</script>

<template>
  <section class="win" aria-labelledby="ib-h">
    <div class="tb">
      <h1 id="ib-h">Inbox</h1>
      <span class="stripes"></span>
      <RouterLink class="wbtn txt" to="/inbox/memory">Memory</RouterLink>
    </div>
    <div class="ib-filters">
      <div class="segk" role="radiogroup" aria-label="State">
        <button v-for="s in states" :key="s.id" type="button" role="radio" :aria-checked="inbox.filters.state === s.id" @click="set({ state: s.id })">{{ s.label }}</button>
      </div>
      <div class="line g8 wrap">
        <label class="ib-sel"><span class="sr">Kind</span>
          <select :value="inbox.filters.kind" @change="onKind">
            <option value="">Tags and covers</option>
            <option value="tag">Tags</option>
            <option value="cover">Covers</option>
          </select>
        </label>
        <label class="ib-sel"><span class="sr">Agent</span>
          <select :value="inbox.filters.connection ?? ''" @change="onAgent">
            <option value="">Every agent</option>
            <option v-for="a in agentOptions" :key="a.id" :value="a.id">{{ a.name }}</option>
          </select>
        </label>
        <span v-if="inbox.filters.album" class="ib-chip"><span class="ell">{{ albumName }}</span><button type="button" class="link" aria-label="Show every album" @click="set({ album: null })">Every album</button></span>
      </div>
    </div>
    <p v-if="inbox.listError" class="bad-text pad" role="alert">{{ inbox.listError }}</p>
    <div v-if="!inbox.listLoading && inbox.proposals.length === 0 && !inbox.listError" class="empty">
      <h2>{{ inbox.filters.state === 'pending' ? 'Nothing to review' : 'No proposals' }}</h2>
      <p>Proposals from a connected agent show here. Status has the setup prompt.</p>
    </div>
    <ul v-else class="rows ib-rows" aria-label="Proposals">
      <li v-for="p in inbox.proposals" :key="p.id" class="row ib-row" :class="{ cur: selected === p.id }">
        <RouterLink class="lib-open two plain" :to="{ path: `/inbox/${p.id}`, query }" :aria-current="selected === p.id ? 'page' : undefined">
          <span class="ttl"><span class="ell">{{ p.title }}</span></span>
          <span class="sub ell">{{ sub(p) }}</span>
          <span class="sub ell">{{ summaryText(p.summary) }}<template v-if="p.stale"> · Changed since proposed</template></span>
        </RouterLink>
        <span class="col g4 r">
          <span class="pill" :class="stateClass(p.state, p.changes_requested)">{{ stateWord(p) }}</span>
          <span class="t2 xs num">{{ finishedAt(p.updated_at, now) }}</span>
        </span>
      </li>
    </ul>
    <div v-if="inbox.next !== null" class="pad"><button type="button" class="key block" @click="inbox.moreList()">Load more</button></div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 14px; }
.rs .wrap { flex-wrap: wrap; }
.rs .ib-filters { display: flex; flex-direction: column; gap: 10px; padding: 12px 14px; border-bottom: 1px solid var(--ink); }
.rs .ib-filters .segk { flex-wrap: wrap; }
.rs .ib-rows { flex: 1; min-height: 0; margin: 0; padding: 0; list-style: none; overflow-y: auto; }
.rs .ib-row { grid-template-columns: minmax(0, 1fr) auto; }
.rs .ib-row .two { gap: 2px; }
.rs .ib-chip { display: inline-flex; align-items: center; gap: 10px; max-width: 100%; min-height: 32px; padding: 0 10px; background: var(--win-2); border: 1px solid var(--ink); border-radius: 4px; font-size: 13px; }
</style>
