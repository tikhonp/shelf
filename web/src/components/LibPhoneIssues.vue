<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useEditor } from '../stores/editor'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import LibIssues from './LibIssues.vue'

defineEmits<{ cover: [] }>()
const editor = useEditor()
const router = useRouter()
const url = computed(() => editor.coverUrl)
</script>

<template>
  <section class="win" aria-labelledby="li-t">
    <div class="tb">
      <button type="button" class="wbtn" aria-label="Back to the album" @click="router.push({ query: {} })"><Icon name="back" /></button>
      <h1 id="li-t">Issues</h1>
      <span class="stripes"></span>
    </div>
    <div v-if="editor.album" class="vscroll">
      <div class="head">
        <Cover :url="url" :size="46" />
        <div class="two"><span class="ttl"><span class="ell">{{ editor.album.title }}</span></span><span class="sub ell">{{ editor.album.artist }}</span></div>
      </div>
      <LibIssues @cover="$emit('cover')" />
    </div>
  </section>
</template>

<style scoped>
.rs .head { display: flex; align-items: center; gap: 12px; padding: 12px; border-bottom: 1px solid var(--ink); }
</style>
