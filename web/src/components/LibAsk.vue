<script setup lang="ts">
import { computed } from 'vue'
import { usePhone } from '../lib/viewport'
import { useEditor } from '../stores/editor'

const phone = usePhone()
const editor = useEditor()
const titles: Record<string, string> = {
  split: 'Split this album?',
  merge: 'Merge these albums?',
  album_artist: 'Rename the album artist?',
  shared_cover: 'Change the shared cover?',
}
const title = computed(() => {
  const w = editor.asks ?? []
  if (w.some((x) => x.kind === 'shared_folder')) return 'Delete these albums?'
  if (w.some((x) => x.kind === 'delete')) return 'Delete this album?'
  return w.length === 1 ? (titles[w[0]!.kind] ?? 'Save anyway?') : 'Save anyway?'
})
</script>

<template>
  <div v-if="editor.asks" :class="{ scrim: !phone }">
    <section class="win" :class="phone ? 'slot tall' : 'dlg'" role="alertdialog" aria-modal="true" aria-labelledby="ask-t" aria-describedby="ask-d">
      <div class="tb"><span class="led warn"></span><h2 id="ask-t">{{ title }}</h2><span class="stripes"></span></div>
      <div id="ask-d" class="ask-body"><p v-for="w in editor.asks" :key="w.kind">{{ w.message }}</p></div>
      <div class="ask-keys">
        <button type="button" class="key" @click="editor.cancelAsks()">Cancel</button>
        <button type="button" class="key sun" @click="editor.confirmAsks()">{{ editor.askKey }}</button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.rs .scrim { position: fixed; inset: 0; z-index: 40; display: flex; align-items: center; justify-content: center; padding: 20px; background: rgba(6, 8, 12, .6); }
.rs .dlg { width: 100%; max-width: 480px; }
.rs .ask-body { display: flex; flex-direction: column; gap: 10px; padding: 18px 18px 14px; }
.rs .ask-keys { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 12px; padding: 0 18px 18px; }
@media (max-width: 719px) {
  .rs .ask-body { padding: 12px 12px 10px; }
  .rs .ask-keys { padding: 0 12px 12px; }
}
</style>
