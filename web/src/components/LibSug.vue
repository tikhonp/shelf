<script setup lang="ts">
import { computed } from 'vue'
import type { Suggestion } from '../api/types'

const props = defineProps<{ s: Suggestion; block?: boolean }>()
defineEmits<{ use: [] }>()
const value = computed(() => (props.s.tracks ? (props.s.proposal ? (props.s.values?.join(', ') || (props.s.values ? 'Remove tag' : 'Per track')) : 'Every track') : (props.s.values ?? []).join(', ')))
const confidence = computed(() => props.s.unrated ? 'Not rated' : `${props.s.confidence}%`)
const from = computed(() => props.s.sources.join(', '))
</script>

<template>
  <button type="button" class="sug" :class="{ block }" :aria-label="`Use ${value}, from ${from}, ${confidence}`" @click="$emit('use')">
    <span class="sug-v">{{ value }}</span><span class="sug-s">{{ from }}</span><span class="sug-p">{{ confidence }}</span>
  </button>
</template>
