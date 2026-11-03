<script setup lang="ts">
import { onMounted, ref } from 'vue'

const version = ref('')
const failed = ref(false)

onMounted(async () => {
  try {
    const res = await fetch('/api/version')
    if (!res.ok) throw new Error(res.statusText)
    const body = (await res.json()) as { version: string }
    version.value = body.version
  } catch {
    failed.value = true
  }
})
</script>

<template>
  <main>
    <h1>Music Shelf</h1>
    <p v-if="version">Version {{ version }}</p>
    <p v-else-if="failed">Couldn't read the version from shelf.</p>
  </main>
</template>
