<script setup lang="ts">
import { ref } from 'vue'
import { mainFields, type MainField, type TagRow } from '../lib/tags'
import { useEditor } from '../stores/editor'
import Icon from './Icon.vue'
import LibAllFields from './LibAllFields.vue'
import LibField from './LibField.vue'
import AlbumProposals from './AlbumProposals.vue'
import LibHead from './LibHead.vue'
import LibIssues from './LibIssues.vue'
import LibLoading from './LibLoading.vue'
import LibLongEditor from './LibLongEditor.vue'
import LibSaveBar from './LibSaveBar.vue'
import LibScope from './LibScope.vue'
import LibUnavailable from './LibUnavailable.vue'

defineEmits<{ close: []; cover: []; history: [] }>()
const editor = useEditor()
const all = ref(false)
const long = ref<TagRow | null>(null)
const mainRow = (f: MainField): TagRow => ({ key: f.label, group: 'standard', target: f.field, fields: [f.field], perTrack: !!f.perTrack, multi: !!f.multi, custom: false })
defineExpose({ toggleAll: () => (all.value = !all.value) })
</script>

<template>
  <section class="win" aria-labelledby="ld-e">
    <div class="tb">
      <h2 id="ld-e">Editor</h2>
      <span class="stripes"></span>
      <button type="button" class="rocker mini" role="switch" :aria-checked="all" @click="all = !all"><span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span><span class="rk-name">All fields</span></button>
      <button type="button" class="wbtn txt" aria-pressed="false" @click="$emit('history')">History</button>
      <button v-if="editor.album" type="button" class="wbtn txt" :disabled="!!editor.saving || !!editor.deleting" @click="editor.remove()">Delete</button>
      <button type="button" class="wbtn" aria-label="Close the editor" @click="$emit('close')"><Icon name="close" /></button>
    </div>
    <LibLoading v-if="editor.loading" />
    <p v-else-if="editor.error" class="bad-text pad" role="alert">{{ editor.error }}</p>
    <template v-else-if="editor.album">
      <LibHead @cover="$emit('cover')" />
      <div class="scope"><LibScope /></div>
      <div class="vscroll">
        <AlbumProposals :album-id="editor.album.id" />
        <LibUnavailable />
        <LibAllFields v-if="all" @long="long = $event" />
        <template v-else>
          <LibIssues @cover="$emit('cover')" />
          <div class="lib-fields"><LibField v-for="f in mainFields" :key="f.field" :f="f" @long="long = mainRow(f)" /></div>
        </template>
      </div>
      <LibSaveBar />
    </template>
    <LibLongEditor v-if="long" :row="long" @close="long = null" />
  </section>
</template>

<style scoped>
.rs .pad { padding: 14px; }
.rs .scope { flex: none; padding: 10px 14px; }
</style>
