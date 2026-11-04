<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import type { Format, Item } from '../api/types'
import { topFrequency } from '../lib/formats'
import { kindWord } from '../lib/format'
import { percent } from '../lib/lamps'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import EqDisplay from './EqDisplay.vue'
import FormatKnob from './FormatKnob.vue'
import FormatMenu from './FormatMenu.vue'
import Icon from './Icon.vue'
import StateLine from './StateLine.vue'

// The Download button of an album, playlist or track, with its format picker.
const props = defineProps<{ item: Item }>()

const phone = usePhone()
const sources = useSources()
const downloads = useDownloads()
const source = computed(() => props.item.ref.source)
const formats = computed(() => sources.formats(source.value))
const chosen = ref(sources.defaultOf(source.value)?.id ?? '')
watch(source, (s) => (chosen.value = sources.defaultOf(s)?.id ?? ''))
const format = computed(() => formats.value.find((f) => f.id === chosen.value))
const menu = ref(false)
const busy = ref(false)

const live = computed(() => downloads.liveStatus(props.item.ref, props.item.status))
const active = computed(() => live.value?.state === 'queued' || live.value?.state === 'running')
const inLibrary = computed(() => live.value?.state === 'in_library')
const noun = computed(() => kindWord(props.item.ref.kind).toLowerCase())
// A forced download fetches only what is missing, so that is what the button says.
const action = computed(() => (inLibrary.value ? 'Fetch missing' : `Download ${noun.value}`))
const top = computed(() => topFrequency(format.value, props.item.quality.sample_rate))
const eqLabel = computed(() => `${format.value?.label}: reaches ${((top.value ?? 0) / 1000).toFixed(1)} kHz`)

async function go() {
  busy.value = true
  await downloads.queue(props.item.ref, { format: chosen.value || undefined, force: inLibrary.value, title: props.item.title, cover: props.item.cover_url })
  busy.value = false
}

function pick(f: Format) {
  chosen.value = f.id
  menu.value = false
}
</script>

<template>
  <div class="col g8">
    <StateLine :status="live" />
    <RouterLink v-if="active" class="key block" :class="{ lg: phone }" :to="`/downloads/${live?.download_id}`">
      {{ live?.state === 'running' ? `Downloading ${percent(live)}%` : 'Queued' }}
    </RouterLink>
    <template v-else-if="phone">
      <div class="menu-at">
        <div class="split block">
          <button type="button" class="key sun lg" :disabled="busy" @click="go"><Icon name="download" />{{ action }}</button>
          <button type="button" class="key sun lg icon" aria-label="Choose format" aria-haspopup="menu" :aria-expanded="menu" @click="menu = !menu"><Icon name="chevron" /></button>
        </div>
        <FormatMenu v-if="menu" :formats="formats" :source="sources.name(source)" :chosen="chosen" @pick="pick" @close="menu = false" />
      </div>
      <p class="t3 sm center">Format: {{ format?.label }}<template v-if="item.ref.kind === 'playlist'"> · a track that doesn't reach it comes in the best quality it has</template></p>
    </template>
    <template v-else>
      <button type="button" class="key sun start" :disabled="busy" @click="go"><Icon name="download" />{{ action }} · {{ format?.label }}</button>
      <div v-if="formats.length" class="panel line g14 wrap">
        <FormatKnob v-model="chosen" :formats="formats" />
        <EqDisplay v-if="item.ref.kind === 'album' && top" :top="top" :label="eqLabel" />
        <p v-else class="t2 sm about">{{ format?.description }}</p>
      </div>
    </template>
  </div>
</template>

<style scoped>
.rs .menu-at { position: relative; }
.rs .start { align-self: flex-start; }
.rs .wrap { flex-wrap: wrap; }
.rs .about { flex: 1 1 150px; }
</style>
