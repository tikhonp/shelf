<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { Item } from '../api/types'
import { releaseCounts } from '../lib/artist'
import { count } from '../lib/format'
import { menuFormats } from '../lib/formats'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import FormatKnob from './FormatKnob.vue'
import Icon from './Icon.vue'

// Download all asks first: on the desktop inside the panel, on the phone in the
// slot above the tab bar. The artist then queues as one download.
const props = defineProps<{ item: Item }>()
const emit = defineEmits<{ close: [] }>()

const phone = usePhone()
const sources = useSources()
const downloads = useDownloads()

const source = computed(() => props.item.ref.source)
const formats = computed(() => menuFormats(sources.formats(source.value)))
const chosen = ref(sources.defaultOf(source.value)?.id ?? '')
const label = computed(() => formats.value.find((f) => f.id === chosen.value)?.label ?? '')
const counts = computed(() =>
  releaseCounts(props.item.sections ?? [], (it) => downloads.liveStatus(it.ref, it.status)?.state === 'in_library'),
)
const waiting = computed(() => downloads.queued.length)
const after = computed(() => (waiting.value ? `after the ${waiting.value} already queued` : ''))
const queuedNote = ref('')

async function confirm() {
  const n = counts.value.toDownload
  const before = after.value
  const d = await downloads.queue(props.item.ref, { format: chosen.value || undefined, title: props.item.title, cover: props.item.cover_url })
  if (!d) return
  const text = `${count(n, 'release')} by ${props.item.title}`
  const sub = [label.value, before].filter(Boolean).join(' · ')
  if (phone.value) {
    downloads.notice = { title: 'Queued', text, sub, cover: props.item.cover_url ?? '', round: true, lamp: '', to: '/downloads' }
    emit('close')
  } else {
    downloads.notice = null
    queuedNote.value = `${count(n, 'download')} queued`
  }
}
</script>

<template>
  <section v-if="phone" class="win slot" aria-label="Download all">
    <div class="tb slot-tb"><h2>Download all</h2><span class="stripes"></span><button type="button" class="wbtn" aria-label="Close" @click="emit('close')"><Icon name="close" /></button></div>
    <div class="col g12 body">
      <p class="ask-line">{{ count(counts.toDownload, 'release') }} by {{ item.title }}.<template v-if="counts.inLibrary"> The {{ counts.inLibrary }} already in your library are skipped.</template></p>
      <div class="segk block" role="radiogroup" aria-label="Format">
        <button v-for="f in formats" :key="f.id" type="button" role="radio" :aria-checked="f.id === chosen" @click="chosen = f.id">{{ f.label }}</button>
      </div>
      <div class="line g10">
        <button type="button" class="key grow" @click="emit('close')">Cancel</button>
        <button type="button" class="key sun main" :disabled="counts.toDownload === 0" @click="confirm">Download {{ counts.toDownload }}</button>
      </div>
    </div>
  </section>

  <div v-else-if="queuedNote" class="panel line g12" role="status">
    <span class="led"></span>
    <div class="two grow"><span class="bold">{{ queuedNote }}</span><span class="sub">{{ [label, after].filter(Boolean).join(' · ') }}</span></div>
    <RouterLink class="key sm" to="/downloads">View</RouterLink>
    <button type="button" class="key sm icon flat" aria-label="Dismiss" @click="emit('close')"><Icon name="close" /></button>
  </div>

  <div v-else class="panel col g14">
    <h4 class="ask">Download {{ count(counts.toDownload, 'release') }} by {{ item.title }}?</h4>
    <div class="line g14 wrap">
      <div class="lcd facts" role="img" :aria-label="`${counts.toDownload} to download, ${counts.inLibrary} in library are skipped`">
        <div v-for="s in counts.sections" :key="s.title" class="lcd-l lcd-row"><span>{{ s.title.toUpperCase() }}</span><span class="num">{{ s.count }}</span></div>
        <div class="lcd-l lcd-row"><span>IN LIBRARY, SKIPPED</span><span class="num">{{ counts.inLibrary }}</span></div>
      </div>
      <FormatKnob v-model="chosen" :formats="formats" :size="56" />
    </div>
    <p class="t2 sm">Each release becomes its own download<template v-if="after">, {{ after }}</template>.</p>
    <div class="line g12">
      <button type="button" class="key grow" @click="emit('close')">Cancel</button>
      <button type="button" class="key sun main" :disabled="counts.toDownload === 0" @click="confirm">Download {{ counts.toDownload }} · {{ label }}</button>
    </div>
  </div>
</template>

<style scoped>
.rs .body { padding: 12px; }
.rs .ask-line { font-size: 15px; line-height: 1.4; }
.rs .ask { margin: 0; font-size: 17px; font-weight: 700; }
.rs .main { flex: 2; }
.rs .wrap { flex-wrap: wrap; }
.rs .facts { flex: 1 1 190px; gap: 4px; }
.rs .segk button { padding: 0 6px; }
</style>
