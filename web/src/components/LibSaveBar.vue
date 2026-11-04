<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { count } from '../lib/format'
import { useEditor } from '../stores/editor'
import { useStatus } from '../stores/status'
import Icon from './Icon.vue'

const editor = useEditor()
const status = useStatus()
const out = computed(() => editor.outcome)
const reason = computed(() => out.value?.reason ?? '')

const big = computed(() => ({ conflict: 'FILES CHANGED', locked: 'LOCKED', busy: 'BUSY' })[reason.value] ?? 'SAVE FAILED')
const small = computed(() => ({ conflict: 'SINCE YOU OPENED', locked: 'OWNED BY ANOTHER USER', busy: 'A DOWNLOAD IS RUNNING' })[reason.value] ?? 'SEE BELOW')
const words = computed(() => {
  switch (reason.value) {
    case 'conflict':
      return 'Another tool changed these files after the album opened. shelf wrote nothing. Reload reads the files again and keeps your staged changes.'
    case 'locked':
      return "shelf can't write files that belong to another user, so it wrote nothing. Your staged changes stay. Give the folder to UID 10001, then press Save again."
    case 'busy':
      return 'A download is writing into this folder. Your staged changes stay. Press Save again when it ends.'
    default:
      return 'Your staged changes stay.'
  }
})
const saved = computed(() => {
  const o = out.value
  if (!o?.ok) return ''
  const what = o.changes > 0 ? `${count(o.changes, 'change')} on ${count(o.tracks, 'track')}.` : 'Cover saved.'
  return status.scan?.enabled ? `${what} Navidrome is scanning.` : what
})
const close = () => (editor.outcome = null)
</script>

<template>
  <div v-if="editor.saving" class="lib-save" role="status">
    <div class="lcd row-lcd"><span class="led run blink"></span><span class="lcd-l">SAVING {{ editor.readable.length }} FILES</span></div>
  </div>
  <div v-else-if="out && !out.ok" class="not-saved" role="alert">
    <div class="tb"><span class="led" :class="reason === 'conflict' || reason === 'locked' || reason === 'busy' ? 'warn' : 'bad'"></span><h3>Not saved</h3><span class="stripes"></span><button type="button" class="wbtn" aria-label="Close" @click="close"><Icon name="close" /></button></div>
    <div class="ns-body">
      <div class="lcd lcd-state ns-lcd"><div class="lcd-big">{{ big }}</div><div class="lcd-l">{{ small }}</div></div>
      <div v-if="!['conflict', 'locked', 'busy'].includes(reason) && out.message" class="term">{{ out.message }}</div>
      <p v-else-if="out.message && reason !== 'conflict'" class="t2 sm">{{ out.message }}</p>
      <p class="t2">{{ words }}</p>
      <button v-if="reason === 'conflict'" type="button" class="key sun block" @click="editor.reload()">Reload album</button>
      <RouterLink v-else-if="reason === 'busy'" class="key block" to="/downloads">Open the download</RouterLink>
      <button v-else-if="reason !== 'locked'" type="button" class="key sun block" @click="editor.save()">Try again</button>
    </div>
  </div>
  <div v-else-if="editor.staged > 0" class="lib-save" role="group" aria-label="Staged changes">
    <div class="lcd"><span class="lcd-l">{{ editor.staged === 1 ? '1 CHANGE' : `${editor.staged} CHANGES` }}</span></div>
    <button type="button" class="key" @click="editor.discard()">Discard</button>
    <button type="button" class="key sun" @click="editor.save()">Save</button>
  </div>
  <div v-else-if="saved" class="saved" role="status">
    <span class="led ok"></span><span class="grow">{{ saved }}</span><button type="button" class="wbtn" aria-label="Close" @click="close"><Icon name="close" /></button>
  </div>
</template>

<style scoped>
.rs .row-lcd { flex-direction: row; align-items: center; justify-content: flex-start; gap: 10px; }
.rs .not-saved { flex: none; max-height: 60%; overflow-y: auto; border-top: 1px solid var(--ink); }
.rs .not-saved h3 { font-size: 16px; }
.rs .ns-body { display: flex; flex-direction: column; gap: 12px; padding: 12px; }
.rs .ns-lcd { min-height: 72px; }
.rs .ns-lcd .lcd-big { font-size: 26px; }
.rs .term { padding: 8px 10px; background: #07090C; border-radius: 4px; color: #C9D2C4; font: 12.5px/1.4 var(--mono); overflow-wrap: anywhere; }
.rs .saved { flex: none; display: flex; align-items: center; gap: 10px; padding: 10px 14px; background: var(--win-2); border-top: 1px solid var(--ink); }
</style>
