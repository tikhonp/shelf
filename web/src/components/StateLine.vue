<script setup lang="ts">
import { computed } from 'vue'
import type { ItemStatus } from '../api/types'
import { stateText } from '../lib/lamps'

const props = defineProps<{ status?: ItemStatus }>()
const st = computed(() => stateText(props.status))
</script>

<template>
  <span v-if="st" class="st"><span class="led" :class="st.led"></span>{{ st.text }}</span>
</template>
