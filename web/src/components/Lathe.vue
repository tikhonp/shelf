<script setup lang="ts">
import { computed } from 'vue'
import { vVars } from '../lib/vars'
import Cover from './Cover.vue'

const props = defineProps<{ size: number; progress: number; cover: string; spin: boolean }>()
const vars = computed(() => ({ '--s': `${props.size}px`, '--p': String(props.progress) }))
</script>

<template>
  <div v-vars="vars" class="lathe" :class="{ spin }" aria-hidden="true">
    <span class="plat"></span><span class="disc"></span><span class="cut"></span>
    <Cover class="label" :url="cover" :size="Math.round(size * 0.4)" />
    <span class="head"></span>
  </div>
</template>

<style scoped>
.rs .lathe { --s: 220px; --p: .5; position: relative; flex: none; width: var(--s); height: var(--s); margin-right: calc(var(--s) * .12); }
.rs .lathe .plat { position: absolute; inset: -3%; border-radius: 50%; background: conic-gradient(from 30deg, #8A9099, #D9DDE2, #8A9099, #5E646D, #8A9099, #D9DDE2, #8A9099); box-shadow: 3px 6px 12px rgba(0, 0, 0, .5), inset 0 0 0 1px rgba(0, 0, 0, .5); }
.rs .lathe .disc { position: absolute; inset: 0; border-radius: 50%; background: radial-gradient(circle at 32% 26%, rgba(255, 255, 255, .22), rgba(255, 255, 255, 0) 42%), radial-gradient(circle closest-side, #232838 0 97%, #3A3F4E 97.5% 100%); box-shadow: inset 0 0 0 1px rgba(0, 0, 0, .6); }
.rs .lathe .cut { position: absolute; inset: 0; border-radius: 50%; background: conic-gradient(rgba(255, 255, 255, 0) 0 30%, rgba(255, 255, 255, .16) 37.5%, rgba(255, 255, 255, 0) 45% 80%, rgba(255, 255, 255, .12) 87.5%, rgba(255, 255, 255, 0) 95%), repeating-radial-gradient(circle closest-side, #0E1018 0 1px, #2A2E3C 1px 2px); -webkit-mask: radial-gradient(circle closest-side, transparent 0 calc((96 - 56 * var(--p)) * 1%), #000 calc((96 - 56 * var(--p)) * 1% + .4%) 96%, transparent 96.4%); mask: radial-gradient(circle closest-side, transparent 0 calc((96 - 56 * var(--p)) * 1%), #000 calc((96 - 56 * var(--p)) * 1% + .4%) 96%, transparent 96.4%); }
.rs .lathe .label { position: absolute; inset: 31%; width: auto; height: auto; border: 1px solid rgba(0, 0, 0, .5); border-radius: 50%; box-shadow: none; }
.rs .lathe::after { content: ""; position: absolute; z-index: 2; left: 50%; top: 50%; width: 4%; height: 4%; transform: translate(-50%, -50%); border-radius: 50%; background: #D9DCE0; }
.rs .lathe.spin .label { animation: rs-spin 1.8s linear infinite; }
.rs .lathe .head { position: absolute; z-index: 3; top: calc(50% - 6px); left: calc(50% + (96 - 56 * var(--p)) * .5% - 6px); right: calc(var(--s) * -.12); height: 12px; border: 1px solid #07090C; border-radius: 3px; background: linear-gradient(180deg, #E1E5EA, #8A9099); box-shadow: 2px 4px 6px rgba(0, 0, 0, .5); transition: left .6s ease; }
.rs .lathe .head::before { content: ""; position: absolute; left: -2px; top: -9px; width: 22px; height: 28px; border: 1px solid #07090C; border-radius: 4px; background: linear-gradient(160deg, #4A5262, #2A2F37); box-shadow: 1px 2px 4px rgba(0, 0, 0, .5); }
.rs .lathe .head::after { content: ""; position: absolute; left: 2px; top: 3px; width: 5px; height: 5px; border-radius: 50%; background: var(--red); box-shadow: 0 0 calc(8px * var(--glow)) var(--red); }
@media (prefers-reduced-motion: reduce) {
  .rs .lathe.spin .label { animation: none; }
  .rs .lathe .head { transition: none; }
}
</style>
