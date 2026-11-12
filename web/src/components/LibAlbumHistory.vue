<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { get } from '../api/client'
import type { Download, DownloadList, LibraryChange } from '../api/types'
import { useNow } from '../lib/clock'
import { ago, count, longDate, resultWord } from '../lib/format'
import { changeTags, changeTitle, reversible, stateClass, stateWord } from '../lib/history'
import { useEditor } from '../stores/editor'
import { useHistory } from '../stores/history'
import { useSources } from '../stores/sources'
import Icon from './Icon.vue'
import LibChangeLines from './LibChangeLines.vue'
import LibChangeNotes from './LibChangeNotes.vue'
import LibTip from './LibTip.vue'

// One album's History, in place of the Editor and the Tracks window.
const props = defineProps<{ albumId: number }>()
defineEmits<{ close: []; editor: [] }>()
const history = useHistory()
const editor = useEditor()
const sources = useSources()
const now = useNow()
const openId = ref<number | null>(null)
const downloads = ref<Download[]>([])

onMounted(() => void history.load(props.albumId))
watch(
  () => props.albumId,
  (id) => {
    openId.value = null
    void history.load(id)
  },
)

// The downloads that wrote into the album's folder come last. Without any,
// or when the call fails, History leaves that part out.
watch(
  () => editor.album?.folder,
  async (folder) => {
    downloads.value = []
    if (!folder) return
    try {
      const page = await get<DownloadList>(`/api/downloads?folder=${encodeURIComponent(folder)}`)
      if (editor.album?.folder === folder) downloads.value = page.downloads
    } catch {
      downloads.value = []
    }
  },
  { immediate: true },
)

function toggle(c: LibraryChange) {
  openId.value = openId.value === c.id ? null : c.id
  if (openId.value !== null && !history.details[c.id]) void history.open(c.id)
}
const busy = (c: LibraryChange) => Object.values(history.running).includes(c.id)
const keyWord = (c: LibraryChange) => `${c.undone ? 'Redo' : 'Undo'}${c.run ? ' run' : ''}`
const runNote = (c: LibraryChange) => {
  if (!c.run) return ''
  const where = c.run.albums === 1 ? 'its album' : `all ${c.run.albums} albums`
  return `${c.undone ? 'Redoes' : 'Undoes'} the run on ${where}.`
}
const downloadedAt = (d: Download) => d.finished_at ?? d.created_at
</script>

<template>
  <section class="win" aria-labelledby="ld-ah">
    <div class="tb">
      <h2 id="ld-ah">History</h2>
      <span class="stripes"></span>
      <span v-if="editor.album" class="on-tb ell">{{ editor.album.title }}</span>
      <button type="button" class="wbtn txt" aria-pressed="true" @click="$emit('editor')">History</button>
      <button type="button" class="wbtn" aria-label="Close the album" @click="$emit('close')"><Icon name="close" /></button>
    </div>
    <p v-if="history.error" class="bad-text pad" role="alert">{{ history.error }}</p>
    <div class="list">
      <div v-if="!history.loading && history.changes.length === 0" class="pad"><div class="lcd lcd-state"><div class="lcd-big">NO SAVES</div></div></div>
      <div v-else class="rows" role="table" :aria-label="editor.album ? `History of ${editor.album.title}` : 'History'">
        <div class="row thead g-ah" role="row">
          <span role="columnheader">When</span>
          <span role="columnheader">Change</span>
          <span role="columnheader" class="r">Tags</span>
          <span role="columnheader">State</span>
          <span role="columnheader"><span class="sr">Actions</span></span>
        </div>
        <template v-for="c in history.changes" :key="c.id">
          <div class="row g-ah" :class="{ cur: openId === c.id }" role="row">
            <span role="cell" class="t2 num sm"><LibTip :note="longDate(c.created_at)">{{ ago(c.created_at, now) }}</LibTip></span>
            <button type="button" class="lib-open two" :aria-expanded="openId === c.id" @click="toggle(c)">
              <span class="ttl"><span class="ell">{{ changeTitle(c) }}</span></span>
              <span class="sub ell"><span class="by">By {{ c.author || 'Unknown' }}</span><template v-if="c.run"> · Run on {{ count(c.run.albums, 'album') }}, undone together</template></span>
            </button>
            <span role="cell" class="num sm r">{{ changeTags(c) }}</span>
            <span role="cell"><span class="pill" :class="stateClass(c)">{{ stateWord(c) }}</span></span>
            <button v-if="reversible(c)" type="button" class="key sm r" :disabled="busy(c)" @click="history.reverse(c)"><LibTip :note="runNote(c)">{{ keyWord(c) }}</LibTip></button>
            <span v-else role="cell"></span>
          </div>
          <LibChangeNotes :c="c" />
          <LibChangeLines v-if="openId === c.id" :c="c" />
        </template>
        <div v-if="history.next !== null" class="pad"><button type="button" class="key block" @click="history.more()">Load more</button></div>
      </div>
      <div v-if="downloads.length > 0" class="rows" role="table" aria-label="Download">
        <div class="lib-h flat"><span>Download</span></div>
        <div v-for="d in downloads" :key="d.id" class="row g-ah" role="row">
          <span role="cell" class="t2 num sm"><LibTip :note="longDate(downloadedAt(d))">{{ ago(downloadedAt(d), now) }}</LibTip></span>
          <span role="cell" class="two">
            <span class="ttl"><span class="ell">Downloaded from {{ sources.name(d.source) }}</span></span>
            <span class="sub ell"><span class="by">By {{ d.author || 'Unknown' }}</span><template v-if="d.format"> · {{ sources.formatLabel(d.source, d.format) }}</template></span>
          </span>
          <span role="cell"></span>
          <span role="cell"><span class="pill" :class="d.status === 'running' ? 'run' : d.status">{{ resultWord(d.status) }}</span></span>
          <RouterLink class="key sm r" :to="`/downloads/${d.id}`">Open</RouterLink>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.rs .list { flex: 1; min-height: 0; overflow-y: auto; scrollbar-width: thin; }
.rs .pad { padding: 14px; }
.rs .flat { position: static; }
.rs .tb .on-tb { min-width: 0; }
</style>
