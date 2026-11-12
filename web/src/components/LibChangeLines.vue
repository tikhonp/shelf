<script setup lang="ts">
import { computed } from 'vue'
import type { LibraryChange } from '../api/types'
import { deletedLines, historyLines } from '../lib/history'
import { useHistory } from '../stores/history'

// An opened History row: what a delete removed, or each tag before and after.
// deep names a file by its album folder too, for a run across many albums.
const props = defineProps<{ c: LibraryChange; deep?: boolean }>()
const history = useHistory()
const lines = computed(() => historyLines(history.details[props.c.id]?.files ?? [], props.deep))
</script>

<template>
  <div v-if="c.kind === 'delete'" class="lib-ba">
    <div v-for="l in deletedLines(c)" :key="l.label" class="row g-del"><span class="t2">{{ l.label }}</span><span class="ell">{{ l.value }}</span></div>
    <p v-if="c.error" class="t2 del-err">{{ c.error }}</p>
  </div>
  <div v-else class="lib-ba">
    <div class="row thead g-ba flat"><span>Track</span><span>Tag</span><span>Before</span><span>After</span></div>
    <div v-for="l in lines" :key="l.track + l.key + l.before" class="row g-ba"><span class="t2 ell">{{ l.track }}</span><span class="mono ell tag">{{ l.key }}</span><span class="t2 ell">{{ l.before || 'Empty' }}</span><span class="ell bold">{{ l.after || 'Empty' }}</span></div>
  </div>
</template>

<style scoped>
.rs .flat { position: static; }
.rs .tag { font-size: 12.5px; }
.rs .g-del { display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 12px; align-items: center; min-height: 34px; padding: 4px 14px; font-size: 13px; }
.rs .del-err { padding: 8px 14px; }
</style>
