<script setup lang="ts">
import type { LibrarySource } from '../api/types'
import { sourceWords } from '../lib/source'

defineProps<{ source: LibrarySource }>()
</script>

<template>
  <p class="sub lib-from">
    <span>{{ sourceWords(source) }}</span>
    <span v-if="source.recorded" class="ell" :title="source.recorded">Recorded in {{ source.recorded }}</span>
    <RouterLink v-if="source.by === 'download' && source.download_id" :to="`/downloads/${source.download_id}`">Download {{ source.download_id }}</RouterLink>
    <a v-if="source.link" :href="source.link" target="_blank" rel="noreferrer">Open on {{ source.name }}</a>
  </p>
</template>

<style scoped>
.rs .lib-from { display: flex; flex-wrap: wrap; gap: 2px 10px; min-width: 0; margin-top: 4px; font-size: 13px; }
.rs .lib-from a { color: inherit; text-decoration: underline; text-underline-offset: 2px; }
</style>
