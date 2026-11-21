<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import type { Preview } from '../api/agents'
import { coverSrc, previewCounts } from '../lib/inbox'
import { inboxNotes } from '../lib/notes'
import Cover from './Cover.vue'
import InboxCoverDialog from './InboxCoverDialog.vue'
import LibTip from './LibTip.vue'

// The authoritative before and after that Shelf computed from the ops, by tag key, or by file and key on request.
const props = defineProps<{ preview: Preview; title: string; note?: string; fields?: number; phone?: boolean; empty?: string }>()
const counts = computed(() => previewCounts(props.preview))

const base = (path: string) => path.split('/').pop() || path
const shown = (vs: string[] | null) => (vs?.length ? vs.join(' / ') : 'Empty')
const each = ref(false)
const expanded = ref<Record<string, boolean>>({})
const isLongVal = (text: string) => text.length > 240 || text.split('\n').length > 5
function toggleExpand(key: string) {
  expanded.value = { ...expanded.value, [key]: !expanded.value[key] }
}

function shortPaths(paths: string[]): Map<string, string> {
  const unique = [...new Set(paths)]
  const parts = new Map(unique.map((p) => [p, p.split('/').filter(Boolean)]))
  const depth = new Map(unique.map((p) => [p, 1]))
  const labelOf = (p: string) => {
    const segs = parts.get(p) ?? []
    const d = depth.get(p) ?? 1
    return segs.length ? segs.slice(-d).join('/') : p
  }
  for (;;) {
    const byLabel = new Map<string, string[]>()
    for (const p of unique) {
      const l = labelOf(p)
      const list = byLabel.get(l)
      if (list) list.push(p)
      else byLabel.set(l, [p])
    }
    let progressed = false
    for (const group of byLabel.values()) {
      if (group.length < 2) continue
      for (const p of group) {
        const d = depth.get(p) ?? 1
        if (d < (parts.get(p)?.length ?? 0)) {
          depth.set(p, d + 1)
          progressed = true
        }
      }
    }
    if (!progressed) break
  }
  return new Map(unique.map((p) => [p, labelOf(p)]))
}

const fileNames = computed(() => shortPaths(props.preview.files.map((f) => f.path)))
const folderNames = computed(() => shortPaths(props.preview.folder.map((c) => c.path)))
const fileName = (path: string) => fileNames.value.get(path) ?? base(path)
const folderName = (path: string) => folderNames.value.get(path) ?? base(path)
const files = computed(() => props.preview.files.filter((f) => f.keys.length > 0))

interface Row { id: string; files: string; title?: string; key: string; before: string; after: string }
// A key every file changes the same way is one row; a key that differs between files keeps a row per file.
const rows = computed<Row[]>(() => {
  const perFile = (key?: string) =>
    props.preview.files.flatMap((f) => f.keys.filter((k) => key === undefined || k.key === key).map((k) => ({ id: `${f.track_id}:${k.key}`, files: fileName(f.path), title: f.path, key: k.key, before: shown(k.before), after: shown(k.after) })))
  if (each.value) return perFile()
  const keys = [...new Set(files.value.flatMap((f) => f.keys.map((k) => k.key)))]
  return keys.flatMap((key) => {
    const changes = files.value.flatMap((f) => f.keys.filter((k) => k.key === key))
    const first = changes[0]!
    const same = changes.every((k) => JSON.stringify(k.before ?? []) === JSON.stringify(first.before ?? []) && JSON.stringify(k.after ?? []) === JSON.stringify(first.after ?? []))
    if (!same) return perFile(key)
    const n = changes.length
    const only = n === 1 ? files.value.find((f) => f.keys.some((k) => k.key === key))! : null
    const where = only ? fileName(only.path) : n === props.preview.files.length ? `All ${n}` : `${n} files`
    return [{ id: key, files: where, title: only?.path, key, before: shown(first.before), after: shown(first.after) }]
  })
})
const grouped = computed(() => !each.value && rows.value.length < props.preview.files.reduce((n, f) => n + f.keys.length, 0))
const artwork = computed(() => [
  { label: 'Folder artwork', files: props.preview.folder.filter((c) => c.before !== c.after).map((c) => ({ path: c.path, label: folderName(c.path), before: c.before, after: c.after })) },
  { label: 'Embedded artwork', files: props.preview.files.filter((f) => f.cover_before !== f.cover_after).map((f) => ({ path: f.path, label: fileName(f.path), before: f.cover_before, after: f.cover_after })) },
].filter((group) => group.files.length))

const artPreview = ref<{ url: string; label: string } | null>(null)
const artTrigger = ref<HTMLElement | null>(null)

function openArtPreview(url: string, previewLabel: string, event: Event) {
  artTrigger.value = (event.currentTarget as HTMLElement) ?? null
  artPreview.value = { url, label: previewLabel }
}
function closeArtPreview() {
  artPreview.value = null
  void nextTick(() => artTrigger.value?.focus())
}
</script>

<template>
  <section class="col ib-diff" :aria-label="title">
    <div class="ib-sec">
      <h3>{{ title }}</h3>
      <span v-if="note" class="t2 sm">{{ note }}</span>
      <span class="grow"></span>
      <button v-if="grouped || each" type="button" class="link sm" :aria-pressed="each" @click="each = !each">{{ each ? 'Group by tag' : 'Show each file' }}</button>
    </div>
    <div v-if="fields != null" class="ib-lcds pad" role="group" :aria-label="title">
      <div class="lcd tight"><span class="lcd-big num">{{ fields }}</span><span class="lcd-l">{{ fields === 1 ? 'FIELD' : 'FIELDS' }}</span></div>
      <div class="lcd tight"><span class="lcd-big num">{{ counts.files }}</span><span class="lcd-l">{{ counts.files === 1 ? 'FILE' : 'FILES' }}</span></div>
      <div class="lcd tight"><span class="lcd-big num">{{ counts.tags }}</span><span class="lcd-l"><LibTip title="Tag writes" :note="phone ? '' : inboxNotes.tagWrites">TAG WRITES</LibTip></span></div>
    </div>
    <p v-if="!rows.length && !artwork.length" class="t2 sm pad">{{ empty ?? 'No file changes.' }}</p>
    <div v-if="rows.length" class="ib-table" role="table" :aria-label="`${title}, tags`">
      <div class="ib-tr head" role="row"><span role="columnheader">Files</span><span role="columnheader">Tag</span><span role="columnheader">Before</span><span role="columnheader">After</span></div>
      <div v-for="r in rows" :key="r.id" class="ib-tr" role="row">
        <span role="cell" class="t2 ib-cell" :title="r.title"><span class="ib-mobile-label">Files</span><span class="ib-cell-v ell">{{ r.files }}</span></span>
        <span role="cell" class="mono ib-cell"><span class="ib-mobile-label">Tag</span><span class="ib-cell-v ell">{{ r.key }}</span></span>
        <span role="cell" class="t2 val">
          <span class="ib-mobile-label">Before</span>
          <span class="ib-val-text" :class="{ expanded: !!expanded[`${r.id}:before`] }">{{ r.before }}</span>
          <button v-if="isLongVal(r.before)" type="button" class="link xs ib-expand" :aria-expanded="!!expanded[`${r.id}:before`]" @click="toggleExpand(`${r.id}:before`)">{{ expanded[`${r.id}:before`] ? 'Show less' : 'Show full' }}</button>
        </span>
        <span role="cell" class="bold val">
          <span class="ib-mobile-label">After</span>
          <span class="ib-val-text" :class="{ expanded: !!expanded[`${r.id}:after`] }">{{ r.after }}</span>
          <button v-if="isLongVal(r.after)" type="button" class="link xs ib-expand" :aria-expanded="!!expanded[`${r.id}:after`]" @click="toggleExpand(`${r.id}:after`)">{{ expanded[`${r.id}:after`] ? 'Show less' : 'Show full' }}</button>
        </span>
      </div>
    </div>
    <section v-for="group in artwork" :key="group.label" :aria-label="group.label">
      <div class="ib-sec"><h3>{{ group.label }}</h3></div>
      <ul class="ib-covers">
        <li v-for="c in group.files" :key="c.path" class="ib-cover">
          <span class="t2 sm ell" :title="c.path">{{ c.label }}</span>
          <span class="line g8">
            <span class="col g4 center">
              <button v-if="c.before" type="button" class="ib-cover-btn" :aria-label="`Inspect ${c.label}, before`" @click="openArtPreview(coverSrc(c.before), `${c.label}, before`, $event)">
                <Cover :url="coverSrc(c.before)" :size="72" />
              </button>
              <Cover v-else :url="coverSrc(c.before)" :size="72" />
              <span class="xs t2">{{ c.before ? 'Before' : 'No cover' }}</span>
            </span>
            <span aria-hidden="true">→</span>
            <span class="col g4 center">
              <button v-if="c.after" type="button" class="ib-cover-btn" :aria-label="`Inspect ${c.label}, after`" @click="openArtPreview(coverSrc(c.after), `${c.label}, after`, $event)">
                <Cover :url="coverSrc(c.after)" :size="72" />
              </button>
              <Cover v-else :url="coverSrc(c.after)" :size="72" />
              <span class="xs t2">{{ c.after ? 'After' : 'Removed' }}</span>
            </span>
          </span>
        </li>
      </ul>
    </section>
    <InboxCoverDialog v-if="artPreview" :url="artPreview.url" :label="artPreview.label" @close="closeArtPreview" />
  </section>
</template>

<style scoped>
.rs .pad { padding: 10px 14px; }
.rs .ib-table { display: flex; flex-direction: column; overflow-x: auto; }
.rs .ib-tr { display: grid; grid-template-columns: minmax(70px, .7fr) minmax(90px, .8fr) minmax(0, 1.2fr) minmax(0, 1.2fr); gap: 10px; align-items: start; min-height: 34px; padding: 6px 14px; border-bottom: 1px solid var(--line); font-size: 13px; }
.rs .ib-tr.head { min-height: 30px; align-items: center; background: var(--win-2); font-family: var(--label); font-size: 12px; font-weight: 600; letter-spacing: .1em; text-transform: uppercase; color: var(--text-3); }
.rs .ib-cell { display: flex; flex-direction: column; min-width: 0; }
.rs .ib-cell-v { min-width: 0; }
.rs .val { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; min-width: 0; }
.rs .ib-val-text { width: 100%; min-width: 0; overflow-wrap: anywhere; white-space: pre-wrap; max-height: 9em; overflow-y: auto; }
.rs .ib-val-text.expanded { max-height: none; overflow-y: visible; }
.rs .ib-expand { align-self: flex-start; }
.rs .ib-covers { display: flex; flex-wrap: wrap; gap: 14px; margin: 0; padding: 10px 14px; list-style: none; }
.rs .ib-cover { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.rs .ib-cover-btn { display: inline-flex; padding: 0; border: 0; border-radius: 3px; background: none; cursor: zoom-in; }
.rs .ib-cover-btn:hover .cv { filter: brightness(1.08); }
.rs .ib-lcds { display: flex; flex-wrap: wrap; gap: 8px; }
.rs .ib-lcds .lcd { flex: 1 1 96px; flex-direction: row; align-items: baseline; gap: 8px; padding: 8px 10px; }
.rs .ib-lcds .lcd-big { font-size: 24px; }
.rs .ib-mobile-label { display: none; }
@media (max-width: 719px) {
  .rs .ib-tr { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
  .rs .ib-tr.head { display: none; }
  .rs .ib-mobile-label { display: block; color: var(--text-2); font-family: var(--font); font-weight: 400; }
  .rs .ib-lcds .lcd { flex-direction: column; gap: 2px; }
}
</style>
