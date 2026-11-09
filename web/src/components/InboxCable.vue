<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

// A patch cable joins each pair of fields that requires links. Its ends sit beside each field's rocker,
// so the component measures the rows, and measures again whenever a row resizes.
export interface CablePair { from: string; to: string; label: string }
const props = defineProps<{ pairs: CablePair[]; bank: HTMLElement | null | undefined; narrow?: boolean }>()

const ys = ref<Record<string, number>>({})
const height = ref(0)
const width = computed(() => (props.narrow ? 38 : 46))
const jack = computed(() => (props.narrow ? 26 : 32))
const ring = computed(() => (props.narrow ? 8 : 9))
const plug = computed(() => (props.narrow ? 11 : 12))
const ids = computed(() => [...new Set(props.pairs.flatMap((p) => [p.from, p.to]))])

let observer: ResizeObserver | null = null
function measure() {
  const bank = props.bank
  if (!bank) return
  const top = bank.getBoundingClientRect().top
  const next: Record<string, number> = {}
  for (const id of ids.value) {
    const row = bank.querySelector<HTMLElement>(`[data-op="${id}"]`)
    const sw = row?.querySelector<HTMLElement>('.ib-op-sw') ?? row
    if (!sw) continue
    const r = sw.getBoundingClientRect()
    next[id] = Math.round(r.top - top + r.height / 2)
  }
  ys.value = next
  height.value = bank.scrollHeight
  bank.querySelectorAll('.ib-op').forEach((el) => observer?.observe(el))
}

onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') observer = new ResizeObserver(() => measure())
  if (props.bank) observer?.observe(props.bank)
  void nextTick(measure)
})
onBeforeUnmount(() => observer?.disconnect())
watch(() => [props.bank, props.pairs], () => {
  if (props.bank) observer?.observe(props.bank)
  void nextTick(measure)
})

const cables = computed(() => props.pairs.flatMap((p, i) => {
  const y1 = ys.value[p.from]
  const y2 = ys.value[p.to]
  if (y1 === undefined || y2 === undefined) return []
  const x = jack.value - 2
  // Each cable bows out a little further than the one before, so cables that cross stay apart.
  const bow = (props.narrow ? 0 : 2) + (i % 3) * 4
  const k = Math.min(20, Math.abs(y2 - y1) / 3)
  const dir = y2 >= y1 ? 1 : -1
  const d = `M${x} ${y1} C ${bow} ${y1 + k * dir}, ${bow} ${y2 - k * dir}, ${x} ${y2}`
  const hi = `M${x - 2} ${y1 + 2 * dir} C ${bow + 2} ${y1 + (k + 1) * dir}, ${bow + 2} ${y2 - (k + 2) * dir}, ${x - 2} ${y2 - 3 * dir}`
  return [{ key: `${p.from}-${p.to}`, d, hi }]
}))
const jacks = computed(() => ids.value.filter((id) => ys.value[id] !== undefined).map((id) => ({ id, y: ys.value[id]! })))
const label = computed(() => props.pairs.map((p) => p.label).join('. '))
</script>

<template>
  <svg class="ib-cable" :width="width" :height="height" :viewBox="`0 0 ${width} ${height}`" role="img" :aria-label="label">
    <path v-for="c in cables" :key="`s-${c.key}`" class="ib-cable-sh" :d="c.d" fill="none" :stroke-width="narrow ? 7 : 8" stroke-linecap="round" transform="translate(2 3)" />
    <path v-for="c in cables" :key="`c-${c.key}`" class="ib-cable-c" :d="c.d" fill="none" :stroke-width="narrow ? 5 : 6" stroke-linecap="round" />
    <path v-for="c in cables" :key="`h-${c.key}`" :d="c.hi" fill="none" stroke="rgba(255,255,255,.28)" stroke-width="1.5" stroke-linecap="round" />
    <g v-for="j in jacks" :key="j.id">
      <circle :cx="jack" :cy="j.y" :r="ring" fill="#0A0C10" stroke="#5D7290" stroke-width="1.5" />
      <rect class="ib-cable-c" :x="jack - plug / 2 - 1" :y="j.y - plug / 2" :width="plug" :height="plug" rx="2.5" stroke="#0A0C10" />
    </g>
  </svg>
</template>

<style scoped>
.ib-cable { position: absolute; left: 0; top: 0; pointer-events: none; overflow: visible; }
.ib-cable-sh { stroke: #0A0C10; opacity: .5; }
path.ib-cable-c { stroke: #D9483B; }
rect.ib-cable-c { fill: #D9483B; }
</style>
