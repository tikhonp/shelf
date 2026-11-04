<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import Cover from './Cover.vue'
import Icon from './Icon.vue'

const shownFor = 8000
const phone = usePhone()
const downloads = useDownloads()
const notice = computed(() => (phone.value ? downloads.notice : downloads.deskNotice))
let timer: ReturnType<typeof setTimeout> | undefined

// A problem waits for a person. Anything else goes by itself.
watch(
  () => downloads.notice,
  (n) => {
    clearTimeout(timer)
    if (n && n.lamp !== 'bad' && n.lamp !== 'warn') timer = setTimeout(() => (downloads.notice = null), shownFor)
  },
  { immediate: true },
)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div v-if="notice" class="win slot" role="status">
    <div class="tb slot-tb">
      <span class="led" :class="notice.lamp"></span>
      <h2>{{ notice.title }}</h2>
      <span class="stripes"></span>
      <button type="button" class="wbtn" aria-label="Dismiss" @click="downloads.notice = null"><Icon name="close" /></button>
    </div>
    <div class="line g12 body">
      <Cover :url="notice.cover" :size="42" :round="notice.round" />
      <div class="two grow"><span class="ell bold">{{ notice.text }}</span><span class="sub ell">{{ notice.sub }}</span></div>
      <RouterLink v-if="notice.to" class="key sm" :to="notice.to">View</RouterLink>
    </div>
  </div>
</template>

<style scoped>
.rs .body { padding: 10px 12px; }
.rs .key.sm { height: 44px; }
</style>
