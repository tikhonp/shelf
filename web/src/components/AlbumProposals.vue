<script setup lang="ts">
import { computed, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useInbox } from '../stores/inbox'

// A link from an album to its pending proposals, only while the library agent is on.
const props = defineProps<{ albumId: number }>()
const inbox = useInbox()
watch([() => props.albumId, () => inbox.enabled], ([id, on]) => { if (on && id) void inbox.loadAlbum(id) }, { immediate: true })
const n = computed(() => (inbox.enabled ? (inbox.byAlbum[props.albumId]?.length ?? 0) : 0))
</script>

<template>
  <RouterLink v-if="n > 0" class="ib-album" :to="{ path: '/inbox', query: { album: String(albumId) } }">{{ n }} {{ n === 1 ? 'proposal' : 'proposals' }}</RouterLink>
</template>

<style scoped>
.rs .ib-album { align-self: flex-start; margin-top: 4px; font-size: 13px; font-weight: 600; }
</style>
