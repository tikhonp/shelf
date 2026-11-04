<script setup lang="ts">
import { computed } from 'vue'
import { useSettings, type NoticeLevel } from '../stores/settings'
import PartCard from './PartCard.vue'

const settings = useSettings()

const levels: { id: NoticeLevel; label: string; about: string }[] = [
  { id: 'off', label: 'Off', about: 'No notice when a download ends.' },
  { id: 'failures', label: 'Failures', about: 'A notice when a download fails or ends with errors.' },
  { id: 'all', label: 'All', about: 'A notice when any download finishes, fails or ends with errors.' },
]
const about = computed(() => levels.find((l) => l.id === settings.notices)?.about)
</script>

<template>
  <PartCard name="Notices" note="In this browser" :lines="[]">
    <div class="col g8 choice">
      <div class="segk block" role="radiogroup" aria-label="Notices">
        <button v-for="l in levels" :key="l.id" type="button" role="radio" :aria-checked="settings.notices === l.id" @click="settings.notices = l.id">{{ l.label }}</button>
      </div>
      <p class="t2 sm">{{ about }}</p>
    </div>
  </PartCard>
</template>

<style scoped>
@media (min-width: 720px) {
  .rs .choice { flex: 1 1 320px; }
}
</style>
