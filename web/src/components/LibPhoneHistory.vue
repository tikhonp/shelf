<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useAlbumHistory } from '../lib/albumHistory'
import { useNow } from '../lib/clock'
import { ago, count, resultWord } from '../lib/format'
import { changeTagWords, changeTitle, stateClass, stateWord } from '../lib/history'
import { historyNote } from '../lib/notes'
import { useEditor } from '../stores/editor'
import { useHelp } from '../stores/help'
import { useHistory } from '../stores/history'
import { useSources } from '../stores/sources'
import Icon from './Icon.vue'
import LibChangeLines from './LibChangeLines.vue'
import LibChangeNotes from './LibChangeNotes.vue'

// One album's History as a page of its own on the phone. The open change's
// key sits in the slot above the tab bar, so LibraryPage gets its ID through
// v-model:open.
const props = defineProps<{ albumId: number }>()
const open = defineModel<number | null>('open', { default: null })
const history = useHistory()
const editor = useEditor()
const help = useHelp()
const sources = useSources()
const router = useRouter()
const now = useNow()
const { downloads, toggle, downloadedAt } = useAlbumHistory(() => props.albumId, open)
const opened = computed(() => history.changes.find((c) => c.id === open.value))
const toggleNote = () => (help.note = help.note ? null : { title: 'History', text: historyNote(opened.value) })
// The note changes with the open change, and closes when the page closes.
watch(opened, (c) => {
  if (help.note?.title === 'History') help.note = { title: 'History', text: historyNote(c) }
})
onBeforeUnmount(() => {
  if (help.note?.title === 'History') help.note = null
})
</script>

<template>
  <section class="win" aria-labelledby="lh-t">
    <div class="tb">
      <button type="button" class="wbtn" aria-label="Back to the album" @click="router.push({ query: {} })"><Icon name="back" /></button>
      <h1 id="lh-t">History</h1>
      <span class="stripes"></span>
      <button type="button" class="wbtn q" aria-label="Note on this page" :aria-pressed="!!help.note" @click="toggleNote">?</button>
    </div>
    <div class="vscroll">
      <div v-if="editor.album" class="lib-h flat top"><span class="ell">{{ editor.album.title }}</span><span class="low ell">{{ editor.album.artist }}</span></div>
      <p v-if="history.error" class="bad-text pad" role="alert">{{ history.error }}</p>
      <div v-if="!history.loading && history.changes.length === 0" class="pad"><div class="lcd lcd-state"><div class="lcd-big">NO SAVES</div></div></div>
      <template v-for="c in history.changes" :key="c.id">
        <button type="button" class="row ah-prow" :class="{ cur: open === c.id }" :aria-expanded="open === c.id" @click="toggle(c)">
          <span class="two"><span class="ttl"><span class="ell">{{ changeTitle(c) }}</span></span><span class="sub ell"><span class="by">By {{ c.author || 'Unknown' }}</span> · {{ ago(c.created_at, now) }}</span></span>
          <span class="lib-q"><span class="pill" :class="stateClass(c)">{{ stateWord(c) }}</span><span class="sub num">{{ changeTagWords(c) }}</span></span>
        </button>
        <LibChangeNotes :c="c" />
        <LibChangeLines v-if="open === c.id" :c="c" stacked>
          <p v-if="c.run" class="ah-note t2 sm">Run on {{ count(c.run.albums, 'album') }}, undone together. The lines below are this album's.</p>
        </LibChangeLines>
      </template>
      <div v-if="history.next !== null" class="pad"><button type="button" class="key block" @click="history.more()">Load more</button></div>
      <template v-if="downloads.length > 0">
        <div class="lib-h flat"><span>Download</span></div>
        <RouterLink v-for="d in downloads" :key="d.id" class="row ah-prow" :to="`/downloads/${d.id}`">
          <span class="two"><span class="ttl"><span class="ell">Downloaded from {{ sources.name(d.source) }}</span></span><span class="sub ell"><span class="by">By {{ d.author || 'Unknown' }}</span> · {{ ago(downloadedAt(d), now) }}</span></span>
          <span class="lib-q"><span class="pill" :class="d.status === 'running' ? 'run' : d.status">{{ resultWord(d.status) }}</span><span v-if="d.format" class="sub num">{{ sources.formatLabel(d.source, d.format) }}</span></span>
        </RouterLink>
      </template>
    </div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 14px; }
.rs .flat { position: static; }
.rs .top { border-top: 0; }
.rs .ah-prow { grid-template-columns: minmax(0, 1fr) auto; width: 100%; margin: 0; border: 0; border-bottom: 1px solid var(--line); border-radius: 0; background: none; color: inherit; font: inherit; text-align: left; text-decoration: none; cursor: pointer; }
.rs .ah-prow.cur { background: var(--sel); }
.rs .ah-prow .lib-q { align-items: flex-end; gap: 4px; }
.rs .ah-note { padding: 8px 14px; border-bottom: 1px solid var(--line); }
</style>
