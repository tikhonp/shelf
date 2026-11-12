<script setup lang="ts">
import { computed } from 'vue'
import type { LibraryChange } from '../api/types'
import { keyWord, reversible, slotWords } from '../lib/history'
import { useHistory } from '../stores/history'

// The phone's slot for the change open in an album's History, where the Save
// bar sits on the album page.
const props = defineProps<{ c: LibraryChange }>()
const history = useHistory()
const words = computed(() => slotWords(props.c, history.details[props.c.id]?.files))
const label = computed(() => `${props.c.undone ? 'Redo' : 'Undo'} ${props.c.run ? 'the run' : 'this Save'}`)
</script>

<template>
  <div v-if="reversible(c)" class="lib-save" role="group" :aria-label="label">
    <div class="lcd"><span class="lcd-l">{{ words }}</span></div>
    <button type="button" class="key" :disabled="history.busy(c)" @click="history.reverse(c)">{{ keyWord(c) }}</button>
  </div>
  <div v-else class="lib-save" role="status">
    <div class="lcd row-lcd"><span class="led run blink"></span><span class="lcd-l">{{ words }}</span></div>
  </div>
</template>

<style scoped>
.rs .row-lcd { flex-direction: row; align-items: center; justify-content: flex-start; gap: 10px; }
</style>
