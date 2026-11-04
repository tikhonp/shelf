<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { backupWords, bulkFixes, fixName } from '../lib/bulk'
import { count, size } from '../lib/format'
import { fixNote } from '../lib/notes'
import { useBulk } from '../stores/bulk'
import { useLibrary } from '../stores/library'
import Icon from './Icon.vue'
import LibTip from './LibTip.vue'

const bulk = useBulk()
const library = useLibrary()
onMounted(() => void bulk.load())
// Select all means what the list finds, so a new search is a new selection.
watch(() => library.total + library.query + library.filter + library.folder, () => {
  if (bulk.all) void bulk.load()
})

const albums = (fix: string) => bulk.preview?.fixes.find((f) => f.fix === fix)?.albums ?? 0
const gb = computed(() => ((bulk.preview?.bytes ?? 0) / 1e9).toFixed(1))
const ready = computed(() => bulk.fixes.length > 0 && (bulk.preview?.albums ?? 0) > 0 && !bulk.starting)
</script>

<template>
  <section class="win" aria-labelledby="ld-b">
    <div class="tb">
      <h2 id="ld-b">Bulk run</h2>
      <span class="stripes"></span>
      <button type="button" class="wbtn" aria-label="Close the bulk run" @click="bulk.close()"><Icon name="close" /></button>
    </div>
    <div class="vscroll body">
      <div class="lib-bank" role="group" aria-label="Fixes to run">
        <button v-for="f in bulkFixes" :key="f" type="button" class="rocker" role="switch" :aria-checked="bulk.fixes.includes(f)" :disabled="albums(f) === 0 && !bulk.fixes.includes(f)" @click="bulk.toggleFix(f)">
          <span class="rk" aria-hidden="true">ON<span class="rk-case"><span class="rk-paddle"></span></span>OFF</span>
          <span class="rk-t"><span class="rk-name"><LibTip :title="fixName(f)" :note="fixNote(f)">{{ fixName(f) }}</LibTip></span><span class="rk-state num">{{ count(albums(f), 'album') }}</span></span>
        </button>
      </div>
      <div class="lib-sum">
        <div class="lcd"><span class="lcd-big num">{{ bulk.preview?.albums ?? 0 }}</span><span class="lcd-l">ALBUMS</span></div>
        <div class="lcd"><span class="lcd-big num">{{ bulk.preview?.files ?? 0 }}</span><span class="lcd-l">FILES</span></div>
        <div class="lcd"><span class="lcd-big num">{{ gb }}</span><span class="lcd-l">GB</span></div>
      </div>
      <p v-if="(bulk.preview?.bytes ?? 0) > 0" class="t2">{{ backupWords(size(bulk.preview!.bytes)) }}</p>
      <p v-if="bulk.error" class="bad-text" role="alert">{{ bulk.error }}</p>
      <div v-if="bulk.preview?.samples.length" class="rows samples" role="table" aria-label="Sample changes">
        <div class="row thead g-smp flat" role="row"><span role="columnheader">Sample change</span><span role="columnheader">Tag</span><span role="columnheader">Before</span><span role="columnheader">After</span></div>
        <div v-for="s in bulk.preview.samples" :key="s.album + s.key" class="row g-smp" role="row">
          <span role="cell" class="ell">{{ s.album }}</span>
          <span role="cell" class="mono ell tag">{{ s.key }}</span>
          <span role="cell" class="t2 ell">{{ s.before || 'Empty' }}</span>
          <span role="cell" class="ell bold">{{ s.after || 'Empty' }}</span>
        </div>
      </div>
    </div>
    <div class="lib-save">
      <span class="t2 grow">The whole run is one undo step.</span>
      <button type="button" class="key sun" :disabled="!ready" @click="bulk.start()">Start</button>
    </div>
  </section>
</template>

<style scoped>
.rs .body { gap: 14px; padding: 14px; }
.rs .samples { border: 1px solid var(--ink); border-radius: 6px; overflow: hidden; }
.rs .flat { position: static; }
.rs .tag { font-size: 12.5px; }
</style>
