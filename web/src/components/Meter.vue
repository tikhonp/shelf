<script setup lang="ts">
import { computed } from 'vue'

// One lamp per track. The API counts tracks, so the lamps show the counts in
// order, not which track failed.
const props = defineProps<{ done: number; failed: number; total: number; running: boolean; label: string }>()

const cells = computed(() => {
  const total = Math.max(props.total, 0)
  return Array.from({ length: total }, (_, i) => {
    if (i < props.done) return 'd'
    if (i < props.done + props.failed) return 'f'
    return props.running && i === props.done + props.failed ? 'c' : ''
  })
})
</script>

<template>
  <div class="meter" role="img" :aria-label="label"><i v-for="(c, i) in cells" :key="i" :class="c"></i></div>
</template>

<style scoped>
.rs .meter { display: flex; gap: 4px; padding: 5px; background: #07090C; border-radius: 4px; box-shadow: inset 1px 1px 3px rgba(0, 0, 0, .9); }
.rs .meter i { flex: 1; height: 10px; background: #1B2A1F; border-radius: 1px; }
.rs .meter i.d { background: var(--green); box-shadow: 0 0 calc(6px * var(--glow)) var(--green); }
.rs .meter i.c { background: var(--sun); box-shadow: 0 0 calc(6px * var(--glow)) var(--sun); animation: rs-blink 1s linear infinite; }
.rs .meter i.f { background: var(--red); box-shadow: 0 0 calc(6px * var(--glow)) var(--red); }
@media (prefers-reduced-motion: reduce) {
  .rs .meter i.c { animation: none; }
}
</style>
