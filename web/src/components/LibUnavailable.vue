<script setup lang="ts">
import { useEditor } from '../stores/editor'

const editor = useEditor()
</script>

<template>
  <div v-for="u in editor.suggestions?.unavailable ?? []" :key="u.source" class="note gap">
    <span class="led warn"></span>
    <div class="two grow"><span class="bold">{{ u.source }} doesn't answer</span><span class="sub">Its suggestions are missing. The others still show.</span></div>
  </div>
  <div v-if="editor.suggestions?.anchor?.reason" class="note gap">
    <span class="led warn"></span>
    <div class="two grow"><span class="bold">{{ editor.suggestions.anchor.title }} doesn't match this album</span><span class="sub">{{ editor.suggestions.anchor.reason }}</span></div>
  </div>
  <div v-if="editor.suggestError" class="note gap">
    <span class="led warn"></span>
    <div class="two grow"><span class="bold">Suggestions are unavailable</span><span class="sub">{{ editor.suggestError }}</span></div>
  </div>
</template>

<style scoped>
.rs .gap { margin: 12px; align-items: center; }
</style>
