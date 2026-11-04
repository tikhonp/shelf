<script setup lang="ts">
import { computed } from 'vue'
import { useNow } from '../lib/clock'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'

const phone = usePhone()
const now = useNow()
const downloads = useDownloads()
const failed = computed(() => downloads.failedSince(now.value))
</script>

<template>
  <div class="counters">
    <template v-if="phone">
      <div class="panel cell"><div class="px num big">{{ downloads.running.length }}</div><div class="xs t2">running</div></div>
      <div class="panel cell"><div class="px num big">{{ downloads.queued.length }}</div><div class="xs t2">queued</div></div>
      <div class="panel cell"><div class="px num big line g8"><span v-if="failed" class="led bad"></span>{{ failed }}</div><div class="xs t2">failed, 24 h</div></div>
    </template>
    <template v-else>
      <div class="lcd row-lcd"><span class="lcd-big num">{{ downloads.running.length }}</span><span class="lcd-l">RUNNING</span></div>
      <div class="lcd row-lcd"><span class="lcd-big num">{{ downloads.queued.length }}</span><span class="lcd-l">QUEUED</span></div>
      <div class="lcd row-lcd"><span class="lcd-big num">{{ failed }}</span><span class="lcd-l">FAILED, 24 H</span></div>
    </template>
  </div>
</template>

<style scoped>
.rs .counters { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.rs .cell { padding: 8px 10px; }
.rs .big { font-size: 24px; line-height: 1; }
.rs .row-lcd { flex-direction: row; justify-content: flex-start; align-items: baseline; gap: 10px; padding: 8px 14px; }
.rs .row-lcd .lcd-big { font-size: 30px; }
@media (max-width: 719px) {
  .rs .counters { gap: 8px; }
}
</style>
