<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Preview } from '../api/agents'
import { coverSrc } from '../lib/inbox'
import Cover from './Cover.vue'

// The authoritative before and after that Shelf computed from the ops, by tag key, or by file and key on request.
const props = defineProps<{ preview: Preview; title: string; note?: string; confirm?: string[] }>()
const emit = defineEmits<{ toggle: [kind: string] }>()

const base = (path: string) => path.split('/').pop() || path
const shown = (vs: string[]) => (vs.length ? vs.join(' / ') : 'Empty')
const each = ref(false)
const files = computed(() => props.preview.files.filter((f) => f.keys.length > 0))

interface Row { id: string; files: string; title?: string; key: string; before: string; after: string }
// A key every file changes the same way is one row; a key that differs between files keeps a row per file.
const rows = computed<Row[]>(() => {
  const perFile = (key?: string) =>
    props.preview.files.flatMap((f) => f.keys.filter((k) => key === undefined || k.key === key).map((k) => ({ id: `${f.track_id}:${k.key}`, files: base(f.path), title: f.path, key: k.key, before: shown(k.before), after: shown(k.after) })))
  if (each.value) return perFile()
  const keys = [...new Set(files.value.flatMap((f) => f.keys.map((k) => k.key)))]
  return keys.flatMap((key) => {
    const changes = files.value.flatMap((f) => f.keys.filter((k) => k.key === key))
    const first = changes[0]!
    const same = changes.every((k) => shown(k.before) === shown(first.before) && shown(k.after) === shown(first.after))
    if (!same) return perFile(key)
    const n = changes.length
    const where = n === 1 ? base(files.value.find((f) => f.keys.some((k) => k.key === key))!.path) : n === props.preview.files.length ? `All ${n}` : `${n} files`
    return [{ id: key, files: where, key, before: shown(first.before), after: shown(first.after) }]
  })
})
const grouped = computed(() => !each.value && rows.value.length < props.preview.files.reduce((n, f) => n + f.keys.length, 0))
const covers = computed(() => [
  ...props.preview.folder.filter((c) => c.before !== c.after).map((c) => ({ id: `f:${c.path}`, where: base(c.path), before: c.before, after: c.after })),
  ...props.preview.files.filter((f) => f.cover_before !== f.cover_after).map((f) => ({ id: `t:${f.track_id}`, where: `Embedded in ${base(f.path)}`, before: f.cover_before, after: f.cover_after })),
])
const empty = computed(() => rows.value.length === 0 && covers.value.length === 0)
</script>

<template>
  <section class="col ib-diff" :aria-label="title">
    <div class="ib-sec">
      <h3>{{ title }}</h3>
      <span v-if="note" class="t2 sm">{{ note }}</span>
      <span class="grow"></span>
      <button v-if="grouped || each" type="button" class="link sm" :aria-pressed="each" @click="each = !each">{{ each ? 'Group by tag' : 'Show each file' }}</button>
    </div>
    <p v-if="empty" class="t2 sm pad">Nothing would change: the files already hold these values.</p>
    <div v-if="rows.length" class="ib-table" role="table" :aria-label="`${title}, tags`">
      <div class="ib-tr head" role="row"><span role="columnheader">Files</span><span role="columnheader">Tag</span><span role="columnheader">Before</span><span role="columnheader">After</span></div>
      <div v-for="r in rows" :key="r.id" class="ib-tr" role="row">
        <span role="cell" class="t2 ell" :title="r.title">{{ r.files }}</span>
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
        <button v-if="confirm" type="button" class="rocker flat" role="switch" :aria-checked="confirm.includes(w.kind)" @click="emit('toggle', w.kind)">
          <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span>
          <span class="ib-warn-t">{{ w.message }}</span>
        </button>
        <span v-else>{{ w.message }}</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 10px 14px; }
.rs .ib-table { display: flex; flex-direction: column; overflow-x: auto; }
.rs .ib-tr { display: grid; grid-template-columns: minmax(70px, .7fr) minmax(90px, .8fr) minmax(0, 1.2fr) minmax(0, 1.2fr); gap: 10px; align-items: start; min-height: 34px; padding: 6px 14px; border-bottom: 1px solid var(--line); font-size: 13px; }
.rs .ib-tr.head { min-height: 30px; align-items: center; background: var(--win-2); font-family: var(--label); font-size: 12px; font-weight: 600; letter-spacing: .1em; text-transform: uppercase; color: var(--text-3); }
.rs .val { min-width: 0; overflow-wrap: anywhere; white-space: pre-wrap; max-height: 9em; overflow-y: auto; }
.rs .ib-covers { display: flex; flex-wrap: wrap; gap: 14px; margin: 0; padding: 10px 14px; list-style: none; }
.rs .ib-cover { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.rs .ib-warn { padding: 6px 10px; border-color: var(--yellow); }
.rs .ib-warn .rocker { min-height: 40px; }
.rs .ib-warn-t { font-size: 14px; white-space: normal; }
@media (max-width: 719px) {
  .rs .ib-tr { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
  .rs .ib-tr.head { display: none; }
}
</style>
