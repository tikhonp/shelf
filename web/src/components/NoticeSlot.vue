<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useDownloads } from '../stores/downloads'
import Cover from './Cover.vue'
import Icon from './Icon.vue'

const shownFor = 8000
const downloads = useDownloads()
let timer: ReturnType<typeof setTimeout> | undefined

watch(
  () => downloads.notice,
  (n) => {
    clearTimeout(timer)
    if (n) timer = setTimeout(() => (downloads.notice = null), shownFor)
  },
)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div v-if="downloads.notice" class="win slot" role="status">
    <div class="tb slot-tb">
      <span class="led" :class="{ bad: downloads.notice.failed }"></span>
      <h2>{{ downloads.notice.title }}</h2>
      <span class="stripes"></span>
      <button type="button" class="wbtn" aria-label="Dismiss" @click="downloads.notice = null"><Icon name="close" /></button>
    </div>
    <div class="line g12 body">
      <Cover :url="downloads.notice.cover" :size="42" :round="downloads.notice.round" />
      <div class="two grow"><span class="ell bold">{{ downloads.notice.text }}</span><span class="sub">{{ downloads.notice.sub }}</span></div>
      <RouterLink v-if="!downloads.notice.failed" class="key sm" to="/downloads">View</RouterLink>
    </div>
  </div>
</template>

<style scoped>
.rs .body { padding: 10px 12px; }
.rs .key.sm { height: 44px; }
</style>
