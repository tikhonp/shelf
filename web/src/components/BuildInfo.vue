<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { get } from '../api/client'
import PartCard from './PartCard.vue'

defineProps<{ last?: boolean }>()

// The version the running binary was built with, so the owner can tell which build is deployed.
const version = ref<string | null>(null)
const failed = ref(false)

onMounted(async () => {
  try {
    version.value = (await get<{ version: string }>('/api/version')).version
  } catch {
    failed.value = true
  }
})

const lines = computed(() => {
  if (version.value) return [`VERSION ${version.value.toUpperCase()}`]
  return [failed.value ? "VERSION DOESN'T ANSWER" : 'READING THE VERSION']
})
</script>

<template>
  <PartCard name="Shelf" :lines="lines" :last="last" />
</template>
