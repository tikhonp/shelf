<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { Format, ItemStatus, Ref } from '../api/types'
import { percent } from '../lib/lamps'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import FormatMenu from './FormatMenu.vue'
import Icon from './Icon.vue'

// The control at the end of a row: Download, then the download's state.
const props = defineProps<{ itemRef: Ref; title: string; status?: ItemStatus; cover?: string; layout: 'phone' | 'wide' | 'narrow' | 'track' }>()

const downloads = useDownloads()
const sources = useSources()
const menu = ref(false)
const busy = ref(false)

const live = computed(() => downloads.liveStatus(props.itemRef, props.status))
const formats = computed(() => sources.formats(props.itemRef.source))
const defaultLabel = computed(() => sources.defaultOf(props.itemRef.source)?.label ?? '')
const sourceName = computed(() => sources.name(props.itemRef.source))
const toDownload = computed(() => `/downloads/${live.value?.download_id}`)
const iconClass = computed(() => (props.layout === 'phone' ? 'big' : props.layout === 'track' ? 'sm flat' : 'sm'))

async function go(format?: Format) {
  menu.value = false
  busy.value = true
  await downloads.queue(props.itemRef, { format: format?.id, title: props.title, cover: props.cover })
  busy.value = false
}
</script>

<template>
  <span v-if="live?.state === 'in_library'" class="mark" :class="{ big: layout === 'phone' }" role="img" aria-label="In library"><Icon name="check" size="l" /></span>
  <RouterLink v-else-if="live?.state === 'running'" class="key icon px" :class="iconClass" :to="toDownload" :aria-label="`Downloading, ${percent(live)} percent. Open the download`">{{ percent(live) }}%</RouterLink>
  <RouterLink v-else-if="live?.state === 'queued'" class="key icon" :class="iconClass" :to="toDownload" aria-label="Queued. Open the download"><Icon name="clock" /></RouterLink>
  <button v-else-if="layout === 'phone'" type="button" class="key sun icon big" :disabled="busy" :aria-label="`Download ${title} in ${defaultLabel}`" :title="`Download ${title} in ${defaultLabel}`" @click="go()">
    <Icon :name="live ? 'retry' : 'download'" size="l" />
  </button>
  <span v-else-if="layout === 'wide'" class="split">
    <button type="button" class="key sm" :disabled="busy" :aria-label="`Download ${title}`" @click="go()"><Icon name="download" size="s" />{{ live ? 'Retry' : 'Download' }}</button>
    <button type="button" class="key sm" aria-label="Choose format" aria-haspopup="menu" :aria-expanded="menu" @click="menu = !menu"><Icon name="chevron" size="s" /></button>
    <FormatMenu v-if="menu" :formats="formats" :source="sourceName" @pick="go" @close="menu = false" />
  </span>
  <button v-else type="button" class="key icon" :class="iconClass" :disabled="busy" :aria-label="`Download ${title}`" :title="`Download ${title}`" @click="go()"><Icon :name="live ? 'retry' : 'download'" size="s" /></button>
</template>

<style scoped>
.rs .key.big { width: 50px; height: 50px; }
.rs .mark { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; color: var(--text-2); }
.rs .mark.big { width: 50px; height: 50px; }
</style>
