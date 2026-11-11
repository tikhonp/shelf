<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { opLabel } from '../lib/inbox'
import { useInbox } from '../stores/inbox'

const props = defineProps<{ albumId: number }>()
const inbox = useInbox()
const proposals = computed(() => inbox.enabled ? (inbox.byAlbum[props.albumId] ?? []).filter((p) => p.state !== 'withdrawn').map((p) => ({ ...p, fields: p.current.ops.filter((op) => p.ops.some((d) => d.id === op.id && d.decision === 'pending')).map(opLabel) })).filter((p) => p.fields.length) : [])
</script>

<template>
  <template v-if="proposals.length">
    <div class="lib-h static"><span>Proposed</span></div>
    <div v-for="p in proposals" :key="p.id" class="lib-iss">
      <span class="led stg" role="img" aria-label="Proposal waiting"></span>
      <span class="proposal-fields"><span class="ttl">{{ p.connection.name }} proposes {{ p.fields.length }} {{ p.fields.length === 1 ? 'field' : 'fields' }}</span><span class="sub">{{ p.fields.join(', ') }}</span></span>
      <RouterLink class="key sm" :to="{ path: `/inbox/${p.id}` }">Review</RouterLink>
    </div>
  </template>
</template>

<style scoped>
.rs .proposal-fields { display: flex; flex-direction: column; gap: 2px; }
.rs .key { text-decoration: none; }
/* On the phone the scope LCD sticks to the top, so a sticky header here would hide behind it. */
.rs .static { position: static; }
</style>
