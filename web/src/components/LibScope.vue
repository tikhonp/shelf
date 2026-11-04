<script setup lang="ts">
import { computed } from 'vue'
import { scopeNote } from '../lib/notes'
import { useEditor } from '../stores/editor'
import LibTip from './LibTip.vue'

const editor = useEditor()
const words = computed(() => {
  const t = editor.selected
  return t ? `TRACK ${String(t.track).padStart(2, '0')} ONLY` : `ALL ${editor.readable.length} TRACKS`
})
</script>

<template>
  <div class="lcd lib-scope"><span class="lcd-l ell"><LibTip title="Which tracks" :note="scopeNote">{{ words }}</LibTip></span></div>
</template>
