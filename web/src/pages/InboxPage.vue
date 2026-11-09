<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import InboxBar from '../components/InboxBar.vue'
import InboxDetail from '../components/InboxDetail.vue'
import InboxList from '../components/InboxList.vue'
import { usePhone } from '../lib/viewport'
import { useAgents } from '../stores/agents'
import { filtersFromQuery, filtersToQuery, useInbox } from '../stores/inbox'

const route = useRoute()
const router = useRouter()
const phone = usePhone()
const inbox = useInbox()
const agents = useAgents()

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
    if (next) void inbox.open(next)
    else inbox.close()
  },
  { immediate: true },
)
</script>

<template>
  <div v-if="phone" class="stack">
    <InboxDetail v-if="id" phone @close="close" />
    <InboxBar v-if="id" phone />
    <InboxList v-else />
  </div>
  <div v-else class="page ib-page">
    <div class="ib-list col"><InboxList :selected="id" /></div>
    <div v-if="id" class="ib-main col"><InboxDetail @close="close" /></div>
  </div>
</template>

<style scoped>
.rs .ib-list { flex: 1 1 380px; min-width: 0; }
.rs .ib-main { flex: 999 1 600px; min-width: 0; }
.rs .ib-page:has(.ib-main) .ib-list { flex: 0 1 420px; }
@media (min-width: 1100px) {
  .rs .ib-page .ib-main { position: sticky; top: 12px; max-height: calc(100dvh - 90px); }
  .rs .ib-page .ib-main > .win { max-height: 100%; }
}
</style>
