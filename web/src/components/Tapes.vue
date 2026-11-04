<script setup lang="ts">
import { computed } from 'vue'
import type { Quality } from '../api/types'
import { tapes } from '../lib/quality'

const props = defineProps<{ quality?: Quality }>()
const list = computed(() => tapes(props.quality))
</script>

<template>
  <span v-if="list.length" class="stks">
    <span v-for="t in list" :key="t.text" class="stk" :class="t.cls">{{ t.text }}</span>
  </span>
</template>
