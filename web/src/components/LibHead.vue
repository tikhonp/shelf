<script setup lang="ts">
import { computed } from 'vue'
import { libraryTape } from '../lib/quality'
import { useEditor } from '../stores/editor'
import Cover from './Cover.vue'

defineEmits<{ cover: [] }>()
const editor = useEditor()
const url = computed(() => (editor.cover?.delete ? '' : (editor.cover?.preview ?? editor.coverUrl)))
</script>

<template>
  <div v-if="editor.album" class="lib-head">
    <button type="button" class="lib-cover" aria-label="Open the cover" title="Cover" @click="$emit('cover')"><Cover :url="url" :size="84" /></button>
    <div class="two grow head-t">
      <h3>{{ editor.album.title }}</h3>
      <span class="sub by">{{ editor.album.artist }}</span>
      <span class="q"><span class="stk" :class="libraryTape(editor.album.quality).cls">{{ libraryTape(editor.album.quality).text }}</span></span>
    </div>
  </div>
</template>

<style scoped>
.rs .head-t { gap: 3px; }
.rs h3 { font-size: 19px; font-weight: 700; line-height: 1.2; }
.rs .by { font-size: 14px; }
.rs .q { margin-top: 5px; }
</style>
