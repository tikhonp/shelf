<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'
import InboxBar from '../components/InboxBar.vue'
import InboxDetail from '../components/InboxDetail.vue'
import InboxList from '../components/InboxList.vue'
import LibSplit from '../components/LibSplit.vue'
import { vVars } from '../lib/vars'
import { usePhone } from '../lib/viewport'
import { useAgents } from '../stores/agents'
import { useHelp } from '../stores/help'
import { filtersFromQuery, filtersToQuery, useInbox } from '../stores/inbox'

const route = useRoute()
const router = useRouter()
const phone = usePhone()
const inbox = useInbox()
const agents = useAgents()
const help = useHelp()

function read(name: string, fallback: number): number {
  try {
    return Number(localStorage.getItem(name)) || fallback
  } catch {
    return fallback
  }
}
function keep(name: string, value: number) {
  try {
    localStorage.setItem(name, String(value))
  } catch {
    // The width then lasts only this visit.
  }
}
const clamp = (v: number) => Math.min(640, Math.max(300, v))
const listW = ref(clamp(read('inbox.list', 420)))
function moveList(d: number) {
  listW.value = clamp(listW.value + d)
  keep('inbox.list', listW.value)
}
const widths = computed(() => ({ '--list-w': `${listW.value}px` }))

const id = computed(() => (route.params.id ? Number(route.params.id) : undefined))
const close = () => void router.push({ path: '/inbox', query: filtersToQuery(inbox.filters) })

// With the feature off the inbox doesn't exist, so its addresses lead back to the library.
watch(
  () => inbox.enabled,
  (on) => {
    if (on === false) void router.replace('/library')
  },
)

onMounted(async () => {
  if (inbox.enabled === null) await inbox.detect()
  if (inbox.enabled) void agents.load()
})

watch(
  () => route.query,
  (q) => {
    if (route.name === 'inbox' || route.name === 'proposal') inbox.setFilters(filtersFromQuery(q))
  },
  { immediate: true },
)

watch(
  id,
  (next) => {
    help.note = null
    if (next) void inbox.open(next)
    else inbox.close()
  },
  { immediate: true },
)
onBeforeUnmount(() => (help.note = null))
</script>

<template>
  <div v-if="phone" class="stack">
    <InboxDetail v-if="id" phone @close="close" />
    <div v-if="id && help.note" class="win slot tall" role="note">
      <div class="lib-note">
        <span class="tip-head"><span class="grow bold">{{ help.note.title }}</span><button type="button" class="wbtn" aria-label="Close the note" @click="help.note = null"><Icon name="close" /></button></span>
        <p class="t2">{{ help.note.text }}</p>
      </div>
    </div>
    <InboxBar v-if="id" phone />
    <InboxList v-else />
  </div>
  <div v-else v-vars="widths" class="page ib-page">
    <div class="ib-list col"><InboxList :selected="id" /></div>
    <LibSplit v-if="id" label="proposal list" @move="moveList" />
    <div v-if="id" class="ib-main col"><InboxDetail @close="close" /></div>
  </div>
</template>

<style scoped>
.rs .ib-list { flex: 1 1 380px; min-width: 0; }
.rs .ib-main { flex: 999 1 600px; min-width: 0; }
@media (max-width: 1099px) {
  .rs .ib-page .ib-list, .rs .ib-page .ib-main { flex: 1 1 100%; }
}
@media (min-width: 1100px) {
  .rs .ib-page { flex-wrap: nowrap; gap: 0; }
  .rs .ib-page .lib-split { align-self: stretch; }
  .rs .ib-page:has(.ib-main) .ib-list { flex: 0 0 var(--list-w); }
  .rs .ib-page .ib-main { flex: 1 1 0; position: sticky; top: 12px; max-height: calc(100dvh - 90px); }
  .rs .ib-page .ib-main > .win { max-height: 100%; }
}
</style>
