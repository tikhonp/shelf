<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { errorText, get } from '../api/client'
import type { Item, Ref } from '../api/types'
import { count, duration, kindWord, releaseDate } from '../lib/format'
import { sameQuality, tapes, words } from '../lib/quality'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import DownloadAll from './DownloadAll.vue'
import Icon from './Icon.vue'
import ItemAction from './ItemAction.vue'
import ItemTitle from './ItemTitle.vue'
import ReleaseActions from './ReleaseActions.vue'
import StateLine from './StateLine.vue'
import Tapes from './Tapes.vue'
import TrackRows from './TrackRows.vue'

const props = defineProps<{ itemRef: Ref }>()
const emit = defineEmits<{ close: []; open: [Item] }>()

const phone = usePhone()
const router = useRouter()
const sources = useSources()
const downloads = useDownloads()
const item = ref<Item | null>(null)
const error = ref('')
const asking = ref(false)

async function load(r: Ref) {
  item.value = null
  error.value = ''
  asking.value = false
  try {
    const found = await get<Item>(`/api/items/${r.source}/${r.kind}/${encodeURIComponent(r.id)}`)
    if (r === props.itemRef) item.value = found
  } catch (err) {
    if (r === props.itemRef) error.value = errorText(err)
  }
}
watch(() => props.itemRef, load, { immediate: true })

const heading = computed(() => kindWord(props.itemRef.kind))
const isGroup = computed(() => props.itemRef.kind === 'artist' || props.itemRef.kind === 'label')
const sourceName = computed(() => sources.name(props.itemRef.source))
const firstTape = computed(() => tapes(item.value?.quality)[0])

const facts = computed(() => {
  const it = item.value
  if (!it) return ''
  const n = it.tracks?.length || it.track_count
  return [it.year ? String(it.year) : '', n ? count(n, 'track') : '', duration(it.duration)].filter(Boolean).join(' · ')
})
const sectionFacts = computed(() =>
  (item.value?.sections ?? [])
    .filter((s) => s.items.length)
    .map((s) => `${s.items.length} ${s.title.toLowerCase()}`)
    .join(' · '),
)
const meta = computed(() => {
  const it = item.value
  if (!it) return []
  return [
    ['Label', it.label],
    ['Release date', releaseDate(it.release_date)],
    ['Genre', it.genre],
    ['UPC', it.upc],
    ['Quality', words(it.quality)],
  ].filter((m): m is [string, string] => !!m[1])
})

const artistSearch = () => void router.push({ path: '/search', query: { q: item.value?.artist, type: 'artist' } })
</script>

<template>
  <section class="win panel-win">
    <div class="tb">
      <button v-if="phone" type="button" class="wbtn" aria-label="Back to results" @click="emit('close')"><Icon name="back" /></button>
      <component :is="phone ? 'h1' : 'h2'">{{ heading }}</component>
      <span class="stripes"></span>
      <a v-if="item?.url" class="wbtn" :href="item.url" target="_blank" rel="noreferrer" :aria-label="`Open on ${sourceName}`"><Icon name="open" /></a>
      <button v-if="!phone" type="button" class="wbtn" :aria-label="`Close the ${heading.toLowerCase()}`" @click="emit('close')"><Icon name="close" /></button>
    </div>
    <div class="vscroll body">
      <div v-if="error" class="note"><span class="led bad"></span><p>{{ error }}</p></div>
      <div v-else-if="!item" class="col g12" aria-busy="true"><span class="sr">Loading</span><span class="sk wide"></span><span class="sk half"></span><span class="sk wide"></span></div>
      <template v-else>
        <div class="line g16 head" :class="{ top: !isGroup }">
          <div class="art">
            <Cover :url="item.cover_url" :size="isGroup ? (phone ? 96 : 112) : phone ? 136 : 150" :round="isGroup" :big="!isGroup" />
            <span v-if="firstTape" class="stk tilt" :class="firstTape.cls">{{ firstTape.text }}</span>
          </div>
          <div class="two g4 grow">
            <span class="src">{{ sourceName }} · {{ heading }}</span>
            <component :is="phone ? 'h2' : 'h3'" class="name line g8"><ItemTitle :title="item.title" :version="item.version" :explicit="item.explicit" /></component>
            <button v-if="item.artist && !isGroup && item.ref.kind !== 'playlist'" type="button" class="link by" @click="artistSearch">{{ item.artist }}</button>
            <span v-else-if="item.artist && !isGroup" class="by bold">{{ item.artist }}</span>
            <span v-if="isGroup" class="sub">{{ sectionFacts }}</span>
            <span v-else class="sub num">{{ facts }}</span>
            <span v-if="phone && item.label" class="sub">{{ item.label }}</span>
          </div>
        </div>

        <template v-if="isGroup">
          <button type="button" class="key sun lg block" :aria-expanded="asking" @click="asking = !asking"><Icon name="download" />Download all</button>
          <DownloadAll v-if="asking && !phone" :item="item" @close="asking = false" />
          <section v-for="s in item.sections ?? []" :key="s.id" class="col">
            <template v-if="s.items.length">
              <div class="sec-h"><h4 class="px">{{ s.title }}</h4><span class="t3 sm num">{{ s.items.length }}</span></div>
              <div class="rows" role="table" :aria-label="s.title">
                <div v-for="a in s.items" :key="a.ref.id" class="row release" role="row">
                  <span role="cell"><Cover :url="a.cover_url" :size="phone ? 52 : 46" /></span>
                  <span role="cell" class="two">
                    <button type="button" class="ttl ttl-btn" @click="emit('open', a)"><ItemTitle :title="a.title" :version="a.version" :explicit="a.explicit" /></button>
                    <span class="sub ell">{{ [a.year, a.track_count ? count(a.track_count, 'track') : ''].filter(Boolean).join(' · ') }}</span>
                    <span v-if="phone" class="line g10 extra"><Tapes :quality="a.quality" /><StateLine :status="downloads.liveStatus(a.ref, a.status)" /></span>
                  </span>
                  <span v-if="!phone" role="cell"><Tapes :quality="a.quality" /></span>
                  <span role="cell"><ItemAction :item-ref="a.ref" :title="a.title" :status="a.status" :cover="a.cover_url" layout="narrow" /></span>
                </div>
              </div>
            </template>
          </section>
        </template>

        <template v-else>
          <ReleaseActions :item="item" />
          <dl v-if="!phone && meta.length" class="meta panel"><template v-for="m in meta" :key="m[0]"><dt>{{ m[0] }}</dt><dd :class="{ mono: m[0] === 'UPC' }">{{ m[1] }}</dd></template></dl>
          <section v-if="item.tracks?.length" class="col">
            <div class="sec-h"><h3 class="px">Tracks</h3><span class="t3 sm num">{{ sameQuality(item.tracks) || item.tracks.length }}</span></div>
            <TrackRows :item="item" />
          </section>
        </template>
      </template>
    </div>
  </section>
  <DownloadAll v-if="asking && phone && item" :item="item" @close="asking = false" />
</template>

<style scoped>
.rs .body { gap: 16px; padding: 16px 14px; }
.rs .head.top { align-items: flex-start; }
.rs .art { position: relative; flex: none; }
.rs .tilt { position: absolute; right: -12px; bottom: 10px; height: 26px; transform: rotate(-6deg); }
.rs .name { font-size: 21px; font-weight: 700; line-height: 1.2; flex-wrap: wrap; }
.rs .name :deep(.ell) { white-space: normal; }
.rs .by { font-size: 15px; font-weight: 600; text-align: left; }
.rs .wide { width: 80%; }
.rs .half { width: 55%; }
.rs .extra { margin-top: 3px; }
.rs .release { grid-template-columns: 46px minmax(0, 1fr) 132px 36px; min-height: 60px; padding: 6px 10px; }
@media (max-width: 719px) {
  .rs .release { grid-template-columns: 52px minmax(0, 1fr) 46px; min-height: 72px; padding: 8px 2px; }
}
@media (min-width: 720px) {
  .rs .panel-win .body { overflow: visible; }
}
</style>
