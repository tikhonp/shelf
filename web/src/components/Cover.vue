<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { coverUrl } from '../lib/covers'
import { vVars } from '../lib/vars'

const props = defineProps<{ url?: string; size: number; round?: boolean; big?: boolean }>()
const failed = ref(false)
const src = computed(() => coverUrl(props.url, props.size))
watch(src, () => (failed.value = false))
</script>

<template>
  <span v-vars="{ '--cv': `${size}px` }" class="cv cover" :class="{ round, big }">
    <img v-if="src && !failed" :src="src" alt="" loading="lazy" @error="failed = true" />
  </span>
</template>

<style scoped>
.rs .cover { display: block; width: var(--cv); height: var(--cv); overflow: hidden; }
.rs .cover img { display: block; width: 100%; height: 100%; object-fit: cover; }
.rs .cover.big { border-width: 2px; border-radius: 6px; box-shadow: 4px 4px 0 var(--ink); }
</style>
