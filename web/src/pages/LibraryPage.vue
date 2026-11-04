<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LibAsk from '../components/LibAsk.vue'
import LibBulk from '../components/LibBulk.vue'
import LibCoverDialog from '../components/LibCoverDialog.vue'
import Icon from '../components/Icon.vue'
import LibEditor from '../components/LibEditor.vue'
import LibGuide from '../components/LibGuide.vue'
import LibHelp from '../components/LibHelp.vue'
import LibHistory from '../components/LibHistory.vue'
import LibList from '../components/LibList.vue'
import LibPhoneAlbum from '../components/LibPhoneAlbum.vue'
import LibPhoneField from '../components/LibPhoneField.vue'
import LibPhoneIssues from '../components/LibPhoneIssues.vue'
import LibPlayer from '../components/LibPlayer.vue'
import LibRun from '../components/LibRun.vue'
import LibSaveBar from '../components/LibSaveBar.vue'
import LibSplit from '../components/LibSplit.vue'
import LibTracks from '../components/LibTracks.vue'
import { libraryAction } from '../lib/libraryKeys'
import { fieldView, isGuide, mainFields, offersFor } from '../lib/tags'
import { usePhone } from '../lib/viewport'
import { vVars } from '../lib/vars'
import { useBulk } from '../stores/bulk'
import { useEditor } from '../stores/editor'
import { useHelp } from '../stores/help'
import { usePlayer } from '../stores/player'

const route = useRoute()
const router = useRouter()
const phone = usePhone()
const editor = useEditor()
const player = usePlayer()
const help = useHelp()
const bulk = useBulk()
const list = ref<InstanceType<typeof LibList>>()
const editorPanel = ref<InstanceType<typeof LibEditor>>()
const playerPanel = ref<InstanceType<typeof LibPlayer>>()

const id = computed(() => (route.params.id ? Number(route.params.id) : undefined))
const field = computed(() => (typeof route.query.field === 'string' ? route.query.field : ''))
const issues = computed(() => route.query.view === 'issues')
const guide = computed(() => route.query.view === 'guide')
const coverOpen = ref(false)
const historyOpen = ref(false)

watch(
  id,
  (next) => {
    player.stop()
    if (next) bulk.panel = false
    if (next) void editor.open(next)
    else editor.close()
  },
  { immediate: true },
)

// A page that opens while a run works shows it, unless it opened on an album.
watch(
  () => bulk.working,
  (working) => {
    if (working && bulk.run?.kind === 'save' && !id.value) bulk.panel = true
  },
  { immediate: true },
)

function stepTrack(by: number) {
  const ts = editor.readable
  const at = ts.findIndex((t) => t.id === editor.trackId)
  const next = at === -1 ? (by > 0 ? 0 : ts.length - 1) : at + by
  editor.trackId = ts[Math.max(0, Math.min(ts.length - 1, next))]?.id ?? null
}

function accept(n: number) {
  const f = mainFields.find((m) => m.field === editor.cursor)
  if (!f) return
  const locked = !!f.perTrack && !editor.selected
  const s = offersFor(editor.suggestions, f.field, editor.trackId, locked, fieldView(editor.scope, f.field, editor.stage).values)[n]
  if (s) editor.take(s, !!f.multi)
}

function addValue() {
  const c = editor.cursor
  if (c && mainFields.find((m) => m.field === c)?.multi) editor.edit(c, [...fieldView(editor.scope, c, editor.stage).values, ''])
}

function fixFirst() {
  const i = editor.album?.issues.find((x) => !isGuide(x.kind) && editor.fixable(x))
  if (i) editor.applyFix(i)
}

function focusCursor() {
  if (!id.value) return list.value?.openRow()
  document.querySelector<HTMLElement>(`[data-target="${editor.cursor}"] input`)?.focus()
}

function leave() {
  editor.noteOn = null
  if (help.sheet) help.sheet = null
  else if (coverOpen.value) coverOpen.value = false
  else if (document.activeElement instanceof HTMLElement && document.activeElement.tagName === 'INPUT') document.activeElement.blur()
  else if (bulk.panel) bulk.panel = false
  else if (historyOpen.value) historyOpen.value = false
  else if (id.value) void router.push({ name: 'library' })
}

const actions: Record<string, () => void> = {
  search: () => list.value?.focusSearch(),
  filter: () => list.value?.toggleFilter(),
  history: () => (historyOpen.value = !historyOpen.value),
  bulk: () => (bulk.panel = !bulk.panel && (bulk.count > 0 || !!bulk.run)),
  'select-row': () => list.value?.pickRow(),
  guide: () => (help.sheet = 'guide'),
  keys: () => (help.sheet = 'keys'),
  'all-fields': () => editorPanel.value?.toggleAll(),
  cover: () => (coverOpen.value = !!id.value),
  revert: () => editor.cursor && editor.drop(editor.cursor),
  'add-value': addValue,
  note: () => (editor.noteOn = editor.noteOn ? null : editor.cursor),
  'suggest-1': () => accept(0),
  'suggest-2': () => accept(1),
  'suggest-3': () => accept(2),
  'track-prev': () => stepTrack(-1),
  'track-next': () => stepTrack(1),
  'track-all': () => (editor.trackId = null),
  play: () => {
    const t = editor.selected ?? player.track ?? editor.readable[0]
    if (t) player.toggle(t)
  },
  back: () => playerPanel.value?.skip(-10),
  forward: () => playerPanel.value?.skip(10),
  save: () => {
    if (editor.staged > 0 && !editor.saving) void editor.save()
  },
  discard: () => editor.discard(),
  fix: fixFirst,
  edit: focusCursor,
  leave,
  'row-up': () => list.value?.moveRow(-1),
  'row-down': () => list.value?.moveRow(1),
}

// The desktop has a key for every action, and the phone has none.
function onKey(e: KeyboardEvent) {
  if (phone.value || editor.asks || editor.deleting) return
  const a = libraryAction(e)
  if (!a) return
  // A key must not act on what an open dialog hides.
  if ((help.sheet || coverOpen.value) && a !== 'leave') return
  e.preventDefault()
  actions[a]?.()
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKey)
  help.sheet = null
  help.note = null
  editor.close()
})

// The Save can give the album a new ID, and the address follows it.
watch(
  () => editor.album?.id,
  (now) => {
    if (now && id.value && now !== id.value) void router.replace({ name: 'album', params: { id: now } })
  },
)

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
const listW = ref(read('library.list', 372))
const tracksW = ref(read('library.tracks', 322))
const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))
function moveList(d: number) {
  listW.value = clamp(listW.value + d, 280, 720)
  keep('library.list', listW.value)
}
function moveTracks(d: number) {
  tracksW.value = clamp(tracksW.value - d, 240, 560)
  keep('library.tracks', tracksW.value)
}
const widths = computed(() => ({ '--list-w': `${listW.value}px`, '--tracks-w': `${tracksW.value}px` }))
const saveSlot = computed(() => editor.staged > 0 || !!editor.saving || !!editor.deleting || !!editor.outcome)

// A deleted album has no page left, so the list takes its place.
watch(
  () => editor.gone,
  (gone) => {
    if (gone === null) return
    editor.gone = null
    if (gone === id.value) void router.push({ name: 'library' })
  },
)
</script>

<template>
  <div v-if="phone" class="stack">
    <LibList v-if="!id" />
    <section v-else-if="guide" class="win" aria-labelledby="lg-t">
      <div class="tb">
        <button type="button" class="wbtn" aria-label="Back to the album" @click="router.push({ query: {} })"><Icon name="back" /></button>
        <h1 id="lg-t">Tagging guide</h1>
        <span class="stripes"></span>
      </div>
      <div class="vscroll"><LibGuide /></div>
    </section>
    <LibPhoneField v-else-if="field" :field="field" />
    <LibPhoneIssues v-else-if="issues" @cover="coverOpen = true" />
    <LibPhoneAlbum v-else @cover="coverOpen = true" />
    <div v-if="id && player.track" class="win slot" role="group" aria-label="Player"><LibPlayer /></div>
    <div v-if="help.note" class="win slot tall" role="note">
      <div class="lib-note">
        <span class="tip-head"><span class="grow bold">{{ help.note.title }}</span><button type="button" class="wbtn" aria-label="Close the note" @click="help.note = null"><Icon name="close" /></button></span>
        <p class="t2">{{ help.note.text }}</p>
        <button type="button" class="link start" @click="router.push({ query: { view: 'guide' } }); help.note = null">Tagging guide</button>
      </div>
    </div>
    <LibAsk v-if="editor.asks" />
    <div v-else-if="id && saveSlot" class="win slot tall"><LibSaveBar /></div>
  </div>
  <main v-else v-vars="widths" class="lib-main">
    <LibList ref="list" class="list" :class="{ wide: !id && !historyOpen && !bulk.panel }" :current="id">
      <template #caps>
        <button type="button" class="wbtn txt" :aria-pressed="bulk.selecting" @click="bulk.toggleSelecting()">Select</button>
        <button type="button" class="wbtn txt" :aria-pressed="historyOpen" @click="historyOpen = !historyOpen">History</button>
      </template>
    </LibList>
    <template v-if="bulk.panel">
      <LibSplit label="album list" @move="moveList" />
      <LibRun v-if="bulk.run" class="editor" />
      <LibBulk v-else class="editor" />
    </template>
    <template v-else-if="historyOpen">
      <LibSplit label="album list" @move="moveList" />
      <LibHistory class="editor" @close="historyOpen = false" />
    </template>
    <template v-else-if="id">
      <LibSplit label="album list" @move="moveList" />
      <LibEditor ref="editorPanel" class="editor" @close="router.push({ name: 'library' })" @cover="coverOpen = true" />
      <LibSplit label="track list" @move="moveTracks" />
      <section class="win tracks" aria-labelledby="ld-t">
        <div class="tb"><h2 id="ld-t">Tracks</h2><span class="stripes"></span></div>
        <LibTracks />
        <LibPlayer ref="playerPanel" />
      </section>
    </template>
    <LibAsk />
  </main>
  <LibCoverDialog v-if="coverOpen" @close="coverOpen = false" />
  <LibHelp />
</template>

<style scoped>
.rs .list { flex: 0 0 var(--list-w, 372px); }
.rs .list.wide { flex: 1 1 0; }
.rs .editor { flex: 1 1 0; }
.rs .tracks { flex: 0 0 var(--tracks-w, 322px); }
.rs .tip-head { display: flex; align-items: center; gap: 8px; }
.rs .start { align-self: flex-start; }
</style>
