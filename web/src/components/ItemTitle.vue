<script setup lang="ts">
defineProps<{ title: string; version?: string; explicit?: boolean }>()
</script>

<template>
  <span class="ell">{{ title }}</span><span v-if="version" class="ver">{{ version }}</span><span v-if="explicit" class="e" title="Explicit" aria-label="Explicit">E</span>
</template>
