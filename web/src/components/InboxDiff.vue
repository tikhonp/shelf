<script setup lang="ts">
import { computed } from 'vue'
import type { Preview } from '../api/agents'
import { coverSrc } from '../lib/inbox'
import Cover from './Cover.vue'

// The authoritative before and after that Shelf computed from the ops, by track and key.
const props = defineProps<{ preview: Preview; title: string; note?: string; confirm?: string[] }>()
const emit = defineEmits<{ toggle: [kind: string] }>()

const base = (path: string) => path.split('/').pop() || path
const shown = (vs: string[]) => (vs.length ? vs.join(' / ') : 'Empty')
const rows = computed(() => props.preview.files.flatMap((f) => f.keys.map((k) => ({ id: `${f.track_id}:${k.key}`, track: base(f.path), key: k.key, before: shown(k.before), after: shown(k.after) }))))
const covers = computed(() => [
  ...props.preview.folder.filter((c) => c.before !== c.after).map((c) => ({ id: `f:${c.path}`, where: base(c.path), before: c.before, after: c.after })),
  ...props.preview.files.filter((f) => f.cover_before !== f.cover_after).map((f) => ({ id: `t:${f.track_id}`, where: `Embedded in ${base(f.path)}`, before: f.cover_before, after: f.cover_after })),
])
const empty = computed(() => rows.value.length === 0 && covers.value.length === 0)
</script>

<template>
  <section class="col ib-diff" :aria-label="title">
    <div class="ib-sec"><h3>{{ title }}</h3><span v-if="note" class="t2 sm">{{ note }}</span></div>
    <p v-if="empty" class="t2 sm pad">Nothing would change: the files already hold these values.</p>
    <div v-if="rows.length" class="ib-table" role="table" :aria-label="`${title}, tags`">
      <div class="ib-tr head" role="row"><span role="columnheader">Track</span><span role="columnheader">Tag</span><span role="columnheader">Before</span><span role="columnheader">After</span></div>
      <div v-for="r in rows" :key="r.id" class="ib-tr" role="row">
        <span role="cell" class="t2 ell" :title="r.track">{{ r.track }}</span>
        <span role="cell" class="mono ell">{{ r.key }}</span>
        <span role="cell" class="t2 val">{{ r.before }}</span>
        <span role="cell" class="bold val">{{ r.after }}</span>
      </div>
    </div>
    <ul v-if="covers.length" class="ib-covers" :aria-label="`${title}, artwork`">
      <li v-for="c in covers" :key="c.id" class="ib-cover">
        <span class="t2 sm ell">{{ c.where }}</span>
        <span class="line g8">
          <span class="col g4 center"><Cover :url="coverSrc(c.before)" :size="72" /><span class="xs t2">{{ c.before ? 'Before' : 'No cover' }}</span></span>
          <span aria-hidden="true">→</span>
          <span class="col g4 center"><Cover :url="coverSrc(c.after)" :size="72" /><span class="xs t2">{{ c.after ? 'After' : 'Removed' }}</span></span>
        </span>
      </li>
    </ul>
    <div v-if="preview.warnings.length" class="col g8 pad">
      <div v-for="w in preview.warnings" :key="w.kind" class="note ib-warn">
        <label v-if="confirm" class="ib-check"><input type="checkbox" :checked="confirm.includes(w.kind)" @change="emit('toggle', w.kind)" /><span>{{ w.message }}</span></label>
        <span v-else>{{ w.message }}</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 10px 14px; }
.rs .ib-table { display: flex; flex-direction: column; overflow-x: auto; }
.rs .ib-tr { display: grid; grid-template-columns: minmax(90px, 1fr) minmax(90px, .8fr) minmax(0, 1.2fr) minmax(0, 1.2fr); gap: 10px; align-items: start; min-height: 34px; padding: 6px 14px; border-bottom: 1px solid var(--line); font-size: 13px; }
.rs .ib-tr.head { min-height: 30px; align-items: center; background: var(--win-2); font-family: var(--label); font-size: 12px; font-weight: 600; letter-spacing: .1em; text-transform: uppercase; color: var(--text-3); }
.rs .val { min-width: 0; overflow-wrap: anywhere; white-space: pre-wrap; max-height: 9em; overflow-y: auto; }
.rs .ib-covers { display: flex; flex-wrap: wrap; gap: 14px; margin: 0; padding: 10px 14px; list-style: none; }
.rs .ib-cover { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.rs .ib-warn { border-color: var(--yellow); }
@media (max-width: 719px) {
  .rs .ib-tr { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
  .rs .ib-tr.head { display: none; }
}
</style>
