<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Suggestion } from '../api/types'
import type { Offers } from '../lib/tags'
import { useEditor } from '../stores/editor'
import LibSug from './LibSug.vue'

const props = defineProps<{ offers: Offers; label: string; block?: boolean }>()
defineEmits<{ use: [s: Suggestion] }>()
const editor = useEditor()

// An opened fold stays open until the field, the album or the track changes.
const stamp = computed(() => `${editor.album?.id}:${editor.trackId}:${props.offers.held}`)
const openAt = ref<string | null>(null)
const open = computed(() => openAt.value === stamp.value)
const n = computed(() => props.offers.rivals.length)
const key = (s: Suggestion) => `${s.field}${s.source}${s.values?.join()}`
</script>

<template>
  <LibSug v-for="s in offers.shown" :key="key(s)" :s="s" :block="block" @use="$emit('use', s)" />
  <template v-if="n > 0">
    <template v-if="open"><LibSug v-for="s in offers.rivals" :key="key(s)" :s="s" :block="block" @use="$emit('use', s)" /></template>
    <button v-else type="button" class="sug fold" :class="{ block }" :aria-label="`Show ${n} other ${n === 1 ? 'suggestion' : 'suggestions'} for ${label}`" @click="openAt = stamp">
      <span class="sug-v">{{ n }} other</span>
    </button>
  </template>
</template>
