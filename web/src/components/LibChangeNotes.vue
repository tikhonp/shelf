<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { proposalOrigin } from '../api/agents'
import type { LibraryChange } from '../api/types'
import { count } from '../lib/format'
import { useHistory } from '../stores/history'
import { useInbox } from '../stores/inbox'

// The lines under a History row: the proposal a Save came from, and the
// fields its last undo or redo left as they were.
const props = defineProps<{ c: LibraryChange }>()
const history = useHistory()
const inbox = useInbox()
const proposal = computed(() => proposalOrigin(props.c.origin ?? ''))
const conflicts = computed(() => history.conflicts[props.c.id])
</script>

<template>
  <p v-if="proposal !== null" class="origin t2 sm">From a reviewed proposal: <RouterLink v-if="inbox.enabled" :to="`/inbox/${proposal}`">proposal {{ proposal }}</RouterLink><span v-else>proposal {{ proposal }}</span></p>
  <p v-if="conflicts" class="conflict t2" role="status">{{ count(conflicts.length, 'field') }} changed since then and stayed: {{ [...new Set(conflicts.map((x) => x.key))].join(', ') }}.</p>
</template>

<style scoped>
.rs .conflict { padding: 8px 14px; border-bottom: 1px solid var(--line); }
.rs .origin { padding: 6px 14px; border-bottom: 1px solid var(--line); }
</style>
