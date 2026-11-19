<script setup lang="ts">
import { RouterLink } from 'vue-router'
import type { Acceptance } from '../api/agents'
import { count } from '../lib/format'
import { outcomeClass, outcomeWord } from '../lib/inbox'

defineProps<{ acceptance: Acceptance; albumId: number; phone?: boolean }>()
</script>

<template>
  <div class="line g8 wrap">
    <span class="pill" :class="outcomeClass(acceptance)">{{ outcomeWord(acceptance) }}</span>
    <span class="t2 sm">
      <template v-if="acceptance.outcome.files">{{ acceptance.outcome.written ?? 0 }} of {{ count(acceptance.outcome.files, 'file') }} written. </template>
      <template v-if="acceptance.outcome.error">{{ acceptance.outcome.error }} </template>
      <span v-if="acceptance.outcome.expired">Change {{ acceptance.change_id }} left the history.</span>
      <RouterLink v-else-if="phone && albumId" :to="{ path: `/library/${albumId}`, query: { view: 'history' } }">Change {{ acceptance.change_id }} in History</RouterLink>
      <span v-else-if="phone">Change {{ acceptance.change_id }} is in History on a desktop.</span>
      <RouterLink v-else :to="{ path: '/library', query: { history: '1' } }">Change {{ acceptance.change_id }} in History</RouterLink>
    </span>
  </div>
</template>
