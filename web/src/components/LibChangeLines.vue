<script setup lang="ts">
import { computed } from 'vue'
import type { LibraryChange } from '../api/types'
import { deletedLines, historyLines } from '../lib/history'
import { useHistory } from '../stores/history'

// An opened History row: what a delete removed, or each tag before and after.
// deep names a file by its album folder too, for a run across many albums.
// stacked puts Before over After, for the phone's narrow page, and shows the
// default slot's content above the lines.
const props = defineProps<{ c: LibraryChange; deep?: boolean; stacked?: boolean }>()
const history = useHistory()
const lines = computed(() => historyLines(history.details[props.c.id]?.files ?? [], props.deep))
</script>

<template>
  <div v-if="c.kind === 'delete'" class="lib-ba">
    <div v-for="l in deletedLines(c)" :key="l.label" class="row g-del"><span class="t2">{{ l.label }}</span><span class="ell">{{ l.value }}</span></div>
    <p v-if="c.error" class="t2 del-err">{{ c.error }}</p>
  </div>
  <div v-else-if="stacked" class="lib-ba">
    <slot />
    <div v-for="l in lines" :key="l.track + l.key + l.before" class="ah-pline"><span class="mono">{{ l.key }} · {{ l.track }}</span><span class="t2">Before</span><span>{{ l.before || 'Empty' }}</span><span class="t2">After</span><span class="bold">{{ l.after || 'Empty' }}</span></div>
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
.rs .ah-pline { display: grid; grid-template-columns: 52px minmax(0, 1fr); gap: 2px 8px; padding: 8px 14px; border-bottom: 1px solid var(--line); font-size: 14px; }
.rs .ah-pline span { overflow-wrap: anywhere; }
.rs .ah-pline .mono { grid-column: 1 / -1; font-size: 12.5px; }
</style>
