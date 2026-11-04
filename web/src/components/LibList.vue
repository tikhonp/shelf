<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { LibraryAlbum } from '../api/types'
import { count } from '../lib/format'
import { typeAngles } from '../lib/formats'
import { libraryTape } from '../lib/quality'
import { isGuide, issueName } from '../lib/tags'
import { usePhone } from '../lib/viewport'
import { useLibrary, type LibrarySort } from '../stores/library'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import Knob from './Knob.vue'
import LibFilterMenu from './LibFilterMenu.vue'

const props = defineProps<{ current?: number }>()
const phone = usePhone()
const library = useLibrary()
const router = useRouter()
const menu = ref(false)

const sorts: { id: LibrarySort; label: string }[] = [
  { id: 'album', label: 'Album' },
  { id: 'artist', label: 'Artist' },
  { id: 'year', label: 'Year' },
  { id: 'added', label: 'Added' },
]
const at = computed(() => sorts.findIndex((s) => s.id === library.sort))
const turn = () => library.setSort(sorts[(at.value + 1) % sorts.length]!.id)

const filterName = computed(() => {
  if (library.folder) return library.folder
  if (library.filter === 'all') return 'All albums'
  if (library.filter === 'issues') return 'Has issues'
  if (library.filter === 'no_mbid') return 'No MusicBrainz ID'
  return issueName(library.filter)
})
const firstIndex = computed(() => library.index && !library.index.ready && library.albums.length === 0)
const none = computed(() => !library.loading && !library.error && library.albums.length === 0 && !firstIndex.value)
const noneWords = computed(() => {
  const q = library.query.trim()
  if (q) return library.filtering ? `Nothing under ${filterName.value} matches "${q}".` : `Nothing matches "${q}".`
  return library.filtering ? `Nothing under ${filterName.value}.` : 'The music folder holds no albums.'
})
const search = ref<HTMLInputElement>()
const row = ref(-1)
watch(() => props.current, (id) => (row.value = library.albums.findIndex((a) => a.id === id)))

// The page calls these for its keys.
function moveRow(by: number) {
  row.value = Math.max(0, Math.min(library.albums.length - 1, row.value + by))
  nextTick(() => document.querySelector('.lib-list .row.kc')?.scrollIntoView({ block: 'nearest' }))
}
function openRow() {
  const a = library.albums[row.value]
  if (a) void open(a)
}
defineExpose({ focusSearch: () => search.value?.focus(), toggleFilter: () => (menu.value = !menu.value), moveRow, openRow })

const open = (a: LibraryAlbum) => router.push({ name: 'album', params: { id: a.id } })
const lit = (a: LibraryAlbum) => a.lamp && a.issues.some((i) => !isGuide(i.kind))
const cover = (a: LibraryAlbum) => `/api/library/albums/${a.id}/cover`
const line = (a: LibraryAlbum) => [a.artist, a.year || ''].filter(Boolean).join(' · ')

onMounted(() => {
  void Promise.allSettled([library.load(), library.loadFilters(), library.loadIndex()])
})
</script>

<template>
  <section class="win" aria-labelledby="lib-t">
    <div class="tb">
      <h1 id="lib-t">Library</h1>
      <span class="stripes"></span>
      <span v-if="library.index?.indexing" class="led run blink" title="Indexing"></span>
      <button v-if="phone" type="button" class="wbtn txt srcbtn" :aria-expanded="menu" aria-haspopup="menu" :aria-label="`Filter: ${filterName}`" @click="menu = !menu"><span class="ell name">{{ filterName }}</span><Icon name="chevron" /></button>
      <slot name="caps" />
    </div>
    <div class="top">
      <label class="field grow">
        <Icon name="search" />
        <span class="sr">Search the library</span>
        <input ref="search" type="search" :value="library.query" :placeholder="phone ? 'Album or artist' : 'Search'" @input="library.search(($event.target as HTMLInputElement).value)" />
        <kbd v-if="!phone" class="px t3 slash">/</kbd>
      </label>
      <button v-if="!phone" type="button" class="key srcbtn tall" :aria-expanded="menu" aria-haspopup="menu" :aria-label="`Filter: ${filterName}`" @click="menu = !menu"><span class="ell">{{ filterName }}</span><Icon name="chevron" /></button>
      <LibFilterMenu v-if="menu && !phone" @close="menu = false" />
    </div>
    <LibFilterMenu v-if="menu && phone" @close="menu = false" />
    <div class="rotary sorts" role="radiogroup" aria-label="Sort by">
      <div class="rot-labels">
        <button v-for="s in sorts.slice(0, 2)" :key="s.id" type="button" role="radio" :aria-checked="s.id === library.sort" @click="library.setSort(s.id)"><span class="led"></span>{{ s.label }}</button>
        <Knob color="grey" :size="phone ? 48 : 44" :angle="typeAngles[at] ?? 0" :label="`Change the sort, now ${sorts[at]?.label}`" @press="turn" />
        <button v-for="s in sorts.slice(2)" :key="s.id" type="button" role="radio" :aria-checked="s.id === library.sort" @click="library.setSort(s.id)"><span class="led"></span>{{ s.label }}</button>
      </div>
    </div>

    <div v-if="firstIndex" class="state">
      <div class="lcd tight"><div class="line g10"><span class="lcd-big num">{{ library.index?.read ?? 0 }}</span><span class="lcd-l">FILES READ</span></div></div>
      <p class="t2 center">shelf is reading the music folder for the first time. Albums appear here when it ends.</p>
    </div>
    <div v-else-if="library.error" class="state">
      <p class="bad-text">{{ library.error }}</p>
      <button type="button" class="key block" @click="library.load()">Try again</button>
    </div>
    <div v-else-if="none" class="state">
      <div class="lcd lcd-state"><div class="lcd-big">NO ALBUMS</div><div v-if="library.filtering" class="lcd-l">FILTER: {{ filterName.toUpperCase() }}</div></div>
      <p class="t2 center">{{ noneWords }}</p>
      <button v-if="library.filtering || library.query" type="button" class="key block" @click="library.clear()">Clear filter and search</button>
    </div>

    <div v-else-if="phone" class="rows list-phone" role="list" aria-label="Albums">
      <div v-for="a in library.albums" :key="a.id" class="row lib-prow" role="listitem">
        <Cover :url="cover(a)" :size="62" />
        <button type="button" class="lib-open two" @click="open(a)">
          <span class="ttl"><span class="ell">{{ a.title }}</span></span>
          <span class="sub ell">{{ line(a) }} · {{ count(a.tracks, 'track') }}</span>
          <span class="q"><span class="stk" :class="libraryTape(a.quality).cls">{{ libraryTape(a.quality).text }}</span></span>
        </button>
        <span v-if="lit(a)" class="led iss" role="img" aria-label="Has issues"></span>
      </div>
      <button v-if="library.hasMore" type="button" class="key block more" :disabled="library.loading" @click="library.more()">Load more</button>
    </div>

    <div v-else class="lib-list" role="grid" aria-label="Albums">
      <div class="lib-h top-h"><span class="num">{{ count(library.total, 'album') }}</span></div>
      <div class="row thead g-lib" role="row">
        <span role="columnheader"><span class="sr">Cover</span></span>
        <span role="columnheader">Album</span>
        <span role="columnheader" class="lib-w">Artist</span>
        <span role="columnheader" class="lib-w">Year</span>
        <span role="columnheader" class="r lib-w">Tracks</span>
        <span role="columnheader">Quality</span>
        <span role="columnheader"><span class="sr">Issues</span></span>
      </div>
      <div v-for="(a, n) in library.albums" :key="a.id" class="row g-lib" :class="{ cur: a.id === current, kc: n === row }" role="row">
        <Cover :url="cover(a)" :size="46" />
        <button type="button" class="lib-open two" @click="open(a)">
          <span class="ttl"><span class="ell">{{ a.title }}</span></span>
          <span class="sub ell lib-n">{{ line(a) }}</span>
        </button>
        <span class="sub ell lib-w">{{ a.artist }}</span>
        <span class="t2 num sm lib-w">{{ a.year || '' }}</span>
        <span class="t2 num sm r lib-w">{{ a.tracks }}</span>
        <span class="lib-q"><span class="stk" :class="libraryTape(a.quality).cls">{{ libraryTape(a.quality).text }}</span><span class="sub num lib-n">{{ count(a.tracks, 'track') }}</span></span>
        <span v-if="lit(a)" class="led iss" role="img" aria-label="Has issues"></span>
      </div>
      <div v-if="library.hasMore" class="more"><button type="button" class="key block" :disabled="library.loading" @click="library.more()">Load more</button></div>
    </div>
  </section>
</template>

<style scoped>
.rs .top { position: relative; display: flex; gap: 10px; padding: 12px 12px 0; }
.rs .top .srcbtn { max-width: 170px; }
.rs .sorts { margin-top: 12px; border-bottom: 0; }
.rs .top-h { border-top: 0; }
.rs .slash { padding: 0 8px; font-size: 15px; }
.rs .row.kc:not(.cur) { outline: 2px solid var(--focus); outline-offset: -2px; }
.rs .name { max-width: 190px; }
.rs .state { flex: 1; display: flex; flex-direction: column; gap: 12px; padding: 12px; border-top: 1px solid var(--ink); }
.rs .list-phone { flex: 1; min-height: 0; overflow-y: auto; scrollbar-width: none; }
.rs .lib-prow .two { gap: 3px; }
.rs .lib-prow .q { margin-top: 4px; }
.rs .more { padding: 12px; }
.rs .list-phone .more { width: calc(100% - 24px); margin: 12px; padding: 0; }
</style>
