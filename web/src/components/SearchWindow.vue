<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { Item, SearchType } from '../api/types'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { typeWords, useSearch, type SourceResults } from '../stores/search'
import { useSources } from '../stores/sources'
import DownloadDeck from './DownloadDeck.vue'
import Icon from './Icon.vue'
import LinkCard from './LinkCard.vue'
import RateWait from './RateWait.vue'
import ResultsGrid from './ResultsGrid.vue'
import ResultsList from './ResultsList.vue'
import SourceSetup from './SourceSetup.vue'
import SourcesSwitch from './SourcesSwitch.vue'
import TypeKnob from './TypeKnob.vue'

const props = defineProps<{ narrow: boolean; cursor: number }>()
const emit = defineEmits<{ go: [string]; open: [Item]; artist: [string]; 'update:cursor': [number]; type: [SearchType] }>()

const typingWait = 400
const phone = usePhone()
const search = useSearch()
const sources = useSources()
const downloads = useDownloads()
const field = ref<HTMLInputElement>()
const sourcesOpen = ref(false)
let typing: ReturnType<typeof setTimeout> | undefined

function input() {
  clearTimeout(typing)
  typing = setTimeout(() => emit('go', search.query.trim()), typingWait)
}
function enter() {
  clearTimeout(typing)
  emit('go', search.query.trim())
}
function clear() {
  clearTimeout(typing)
  search.clear()
  emit('go', '')
  field.value?.focus()
}
async function paste() {
  try {
    search.query = (await navigator.clipboard.readText()).trim()
    enter()
  } catch {
    // The browser refused, so a long press in the field is the way.
    field.value?.focus()
  }
}
onBeforeUnmount(() => clearTimeout(typing))
defineExpose({ focus: () => field.value?.focus() })

const words = computed(() => typeWords[search.type])
const shown = computed(() => `${search.items.length} of ${search.total} ${search.total === 1 ? words.value.one : words.value.many}`)
const names = computed(() => search.on.map((s) => s.name).join(' and '))
const hasResults = computed(() => search.items.length > 0)
const answered = computed(() => search.results.filter((r) => !r.error && !r.loading))
const waitingOn = computed(() => search.results.filter((r) => r.loading).map((r) => sources.name(r.id)).join(' and '))
const firstRun = computed(() => sources.noneReady && sources.list.length > 0 && !search.ran)
const othersOff = computed(() => sources.enabled.filter((s) => search.off.includes(s.id)))

// reason is the part of a source's error after its name, for the display.
function reason(r: SourceResults) {
  const name = sources.name(r.id)
  return r.error.startsWith(`${name}: `) ? r.error.slice(name.length + 2) : r.error
}
function fromOthers(r: SourceResults) {
  const others = answered.value.map((x) => sources.name(x.id))
  return others.length ? `Results come from ${others.join(' and ')} only.` : ''
}
const limited = computed(() => search.failed.find((r) => r.rateLimited))
</script>

<template>
  <section class="win" :class="{ 'page-main': !phone }" aria-label="Search">
    <div class="tb">
      <h1>Search</h1>
      <span class="stripes"></span>
      <span v-if="!phone && hasResults" class="t3 sm num">{{ shown }}</span>
      <SourcesSwitch v-if="phone && !search.link" v-model:open="sourcesOpen" part="button" small panel-id="sources" />
    </div>

    <div class="col g10 top">
      <SourcesSwitch v-if="phone && sourcesOpen" part="rockers" small panel-id="sources" />
      <div class="line g10 wrap">
        <label class="field grow">
          <Icon name="search" size="l" />
          <span class="sr">Search, or paste a Qobuz or Apple Music link</span>
          <input ref="field" v-model="search.query" type="search" :disabled="firstRun" :placeholder="firstRun ? 'Set up a source to search' : phone ? 'Search or paste a link' : 'Search, or paste a Qobuz or Apple Music link'" enterkeyhint="search" @input="input" @keydown.enter.prevent="enter" />
          <kbd v-if="!phone && !search.query" class="px t3 slash">/</kbd>
          <button v-if="search.query" type="button" class="key flat icon" aria-label="Clear" @click="clear"><Icon name="close" /></button>
        </label>
        <button v-if="phone && !search.query && !firstRun" type="button" class="key icon tall" aria-label="Paste a link" title="Paste a link" @click="paste"><Icon name="paste" size="l" /></button>
        <SourcesSwitch v-if="!phone" v-model:open="sourcesOpen" part="button" panel-id="sources" />
      </div>
      <SourcesSwitch v-if="!phone && sourcesOpen" part="rockers" panel-id="sources" />
    </div>

    <TypeKnob v-if="!search.link && !firstRun" :model-value="search.type" @update:model-value="emit('type', $event)" />

    <div class="vscroll fill">
      <!-- No source ready -->
      <div v-if="firstRun" class="col g12 state">
        <div class="lcd lcd-state"><div class="lcd-big">NO SOURCE READY</div><div class="lcd-l">SET UP QOBUZ OR APPLE MUSIC</div></div>
        <SourceSetup v-for="s in sources.list" :key="s.id" :source="s" />
      </div>

      <!-- Every source switched off -->
      <div v-else-if="search.noSources" class="empty"><h2>No sources on</h2><p>Turn on at least one source to search.</p></div>

      <!-- A pasted link -->
      <div v-else-if="search.link" class="state">
        <div v-if="search.link.loading" class="lcd line g10 busy"><span class="led run blink"></span><span class="lcd-l">READING THE LINK</span></div>
        <LinkCard v-else-if="search.link.item" :item="search.link.item" @open="emit('open', $event)" @artist="emit('artist', $event)" />
        <div v-else class="col g12">
          <div class="lcd lcd-state"><div class="lcd-big">UNKNOWN LINK</div><div class="lcd-l">NOT A LINK SHELF CAN READ</div></div>
          <p class="t2 center">{{ search.link.error }}</p>
          <button type="button" class="key block" @click="clear">Clear</button>
        </div>
      </div>

      <!-- Before the first search -->
      <div v-else-if="!search.ran" class="col g12 state">
        <div class="empty"><h2>Search {{ names }}</h2><p>Type an album, an artist or a track, or paste a link from either app.</p></div>
        <template v-if="phone"><DownloadDeck v-for="d in downloads.running" :key="d.id" :download="d" /></template>
      </div>

      <template v-else>
        <div v-if="search.loading && !hasResults" class="col g12 state">
          <div class="lcd line g10 busy"><span class="led run blink"></span><span class="lcd-l">SEARCHING {{ waitingOn.toUpperCase() }}</span></div>
          <div class="col g12" aria-busy="true">
            <span class="sr">Loading results</span>
            <div v-for="n in 3" :key="n" class="line g12"><span class="sk cover"></span><span class="col g8 grow"><span class="sk w80"></span><span class="sk w50"></span></span></div>
          </div>
        </div>

        <template v-else>
          <div v-for="r in hasResults ? search.failed : []" :key="r.id" class="note warn" role="status">
            <span class="led" :class="r.rateLimited ? 'warn' : 'bad'"></span>
            <p>{{ r.error }}. {{ fromOthers(r) }} <RouterLink to="/status">Status</RouterLink></p>
          </div>

          <template v-if="hasResults">
            <ResultsList v-if="phone" :items="search.items" :label="words.many" @open="emit('open', $event)" />
            <ResultsGrid v-else :items="search.items" :type="search.type" :label="words.many" :narrow="narrow" :cursor="cursor" @open="emit('open', $event)" @update:cursor="emit('update:cursor', $event)" />
            <div class="line g12 between foot">
              <span v-if="phone" class="t3 sm num">{{ shown }}</span>
              <span v-else class="t3 sm">↑ ↓ move · Enter opens · Esc closes · / searches</span>
              <button v-if="search.hasMore" type="button" class="key" :disabled="search.loading" @click="search.loadMore()">Load more</button>
            </div>
          </template>

          <div v-else-if="limited" class="state"><RateWait :message="limited.error" @retry="search.submit()" /></div>

          <div v-else-if="search.failed.length" class="col g12 state">
            <template v-for="r in search.failed" :key="r.id">
              <div class="lcd lcd-state"><div class="lcd-big">{{ reason(r).toUpperCase() }}</div><div class="lcd-l">{{ sources.name(r.id).toUpperCase() }}</div></div>
              <p class="t2 center">{{ sources.name(r.id) }} can't search right now. {{ fromOthers(r) }}</p>
            </template>
            <RouterLink class="key block" to="/status">Open Status</RouterLink>
          </div>

          <div v-else class="col g12 state">
            <div class="lcd lcd-state"><div class="lcd-big">NO RESULTS</div><div class="lcd-l">0 {{ words.many.toUpperCase() }} ON {{ names.toUpperCase() }}</div></div>
            <p class="t2 center">Nothing found for "{{ search.ran }}". Check the spelling<template v-if="othersOff[0]">, or look on {{ othersOff[0].name }}</template>.</p>
            <button v-if="othersOff[0]" type="button" class="key block" @click="search.toggle(othersOff[0].id)">Search {{ othersOff[0].name }}</button>
          </div>
        </template>
      </template>
    </div>
    <span class="sr" role="status">{{ hasResults ? shown : '' }}</span>
  </section>
</template>

<style scoped>
.rs .top { padding: 12px; }
.rs .slash { display: block; padding: 0 8px; font-size: 15px; }
.rs .key.icon.tall { width: 50px; height: 50px; }
.rs .fill { flex: 1; min-height: 0; }
.rs .state { padding: 12px; }
.rs .busy { flex: none; }
.rs .cover { width: 52px; height: 52px; }
.rs .w80 { width: 80%; }
.rs .w50 { width: 55%; }
.rs .note { margin: 0 12px 12px; }
.rs .foot { padding: 14px; flex-wrap: wrap; }
@media (min-width: 720px) {
  .rs .fill { overflow-y: visible; }
  .rs .top { padding: 14px; }
}
@media (max-width: 719px) {
  .rs .field { flex: 1 1 0; }
}
</style>
