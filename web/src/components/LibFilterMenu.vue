<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { isGuide, issueName } from '../lib/tags'
import { useLibrary } from '../stores/library'
import Icon from './Icon.vue'

const emit = defineEmits<{ close: [] }>()
const library = useLibrary()
const root = ref<HTMLElement>()

interface Entry {
  label: string
  filter: string
  folder?: string
  source?: string
  count: number
}

const groups = computed(() => {
  const f = library.filters
  if (!f) return []
  const kinds = (guide: boolean) => f.kinds.filter((k) => isGuide(k.name) === guide && k.albums > 0).map<Entry>((k) => ({ label: issueName(k.name), filter: k.name, count: k.albums }))
  return [
    { name: 'Show', entries: [{ label: 'All albums', filter: 'all', count: f.albums }, { label: 'Has issues', filter: 'issues', count: f.issues }] },
    { name: 'Issues', entries: kinds(false) },
    { name: 'Guide', entries: [...kinds(true), { label: 'No MusicBrainz ID', filter: 'no_mbid', count: f.no_mbid }] },
    {
      name: 'Sources',
      entries: [
        ...f.sources.map<Entry>((s) => ({ label: s.name, filter: 'all', source: s.id, count: s.albums })),
        ...(f.no_source > 0 ? [{ label: 'No known source', filter: 'all', source: 'none', count: f.no_source }] : []),
      ],
    },
    { name: 'Folders', entries: f.folders.map<Entry>((d) => ({ label: d.name, filter: 'all', folder: d.name, count: d.albums })) },
  ].filter((g) => g.entries.length > 0)
})

const isOn = (e: Entry) => e.filter === library.filter && (e.folder ?? '') === library.folder && (e.source ?? '') === library.source
function pick(e: Entry) {
  library.setFilter(e.filter, e.folder, e.source)
  emit('close')
}

function outside(e: Event) {
  if (root.value && !root.value.parentElement?.contains(e.target as Node)) emit('close')
}
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.stopPropagation()
    emit('close')
  }
}
onMounted(() => {
  document.addEventListener('pointerdown', outside)
  document.addEventListener('keydown', key, true)
  root.value?.querySelector('button')?.focus()
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', key, true)
})
</script>

<template>
  <div ref="root" class="pop" role="menu" aria-label="Filter">
    <template v-for="g in groups" :key="g.name">
      <div class="pop-h">{{ g.name }}</div>
      <button v-for="e in g.entries" :key="e.label" type="button" role="menuitemradio" :aria-checked="isOn(e)" :class="{ hl: isOn(e) }" @click="pick(e)">
        <span class="ck"><Icon v-if="isOn(e)" name="check" /></span>
        <span class="ell">{{ e.label }}</span>
        <span class="hint num">{{ e.count.toLocaleString('en-US') }}</span>
      </button>
    </template>
  </div>
</template>

<style scoped>
.rs .pop { top: 66px; left: 12px; right: 12px; max-height: min(520px, calc(100dvh - 180px)); overflow-y: auto; scrollbar-width: thin; }
@media (max-width: 719px) {
  .rs .pop { top: 60px; left: 8px; right: 8px; bottom: 8px; max-height: none; }
}
</style>
