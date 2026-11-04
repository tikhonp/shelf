<script setup lang="ts">
import { computed } from 'vue'
import { vVars } from '../lib/vars'

const props = defineProps<{ color: 'red' | 'blue'; size: number; angle: number; label: string }>()
defineEmits<{ press: [] }>()
const vars = computed(() => ({ '--s': `${props.size}px`, '--a': `${props.angle}deg` }))
</script>

<template>
  <button v-vars="vars" type="button" class="knob" :class="color" :aria-label="label" title="Press to turn" @click="$emit('press')"><i></i></button>
</template>

<style scoped>
.rs .knob { --s: 48px; --a: 0deg; --ring: 0px;
  --tooth: #9AA1A9;
  --cap: radial-gradient(circle at 38% 30%, rgba(255, 255, 255, .28), rgba(255, 255, 255, 0) 40%), radial-gradient(circle closest-side, #A9B0B8 0 72%, #7E858E 76%, #C6CCD2 81%, #A7AEB6 92%, #7C838C 100%);
  --ptr: #101216; --ptr-w: 2px; --ptr-len: .2;
  position: relative; flex: none; display: inline-block; width: var(--s); height: var(--s); border-radius: 50%; }
.rs .knob > i { position: absolute; inset: var(--ring); border-radius: 50%; background: radial-gradient(circle at 30% 22%, rgba(255, 255, 255, .55), rgba(255, 255, 255, 0) 42%), radial-gradient(circle closest-side, #0A0F18 0 76%, #2A2E35 78%, transparent 79.5%), conic-gradient(from -90deg, #F1F3F5, #DADDE1 60deg, #B9BDC3 110deg, #8B9097 160deg, #6A6E75 205deg, #979BA2 245deg, #D6D9DD 300deg, #F1F3F5); box-shadow: inset 0 0 0 1px rgba(0, 0, 0, .6), 2px 4px 7px rgba(0, 0, 0, .55), 0 1px 1px rgba(0, 0, 0, .4); transition: transform .08s ease; }
.rs .knob > i::before { content: ""; position: absolute; inset: 0; border-radius: 50%; background: repeating-conic-gradient(var(--tooth) 0 8deg, #0A0F18 8deg 15deg); -webkit-mask: radial-gradient(circle closest-side, transparent 64%, #000 64% 74.5%, transparent 75%); mask: radial-gradient(circle closest-side, transparent 64%, #000 64% 74.5%, transparent 75%); }
.rs .knob > i::after { content: ""; position: absolute; inset: 16%; border-radius: 50%; background: var(--cap); box-shadow: inset 0 -2px 3px rgba(0, 0, 0, .3), 0 0 0 1px rgba(5, 10, 20, .6); }
.rs .knob::after { content: ""; position: absolute; z-index: 1; left: calc(50% - var(--ptr-w) / 2); top: calc(var(--ring) + (var(--s) / 2 - var(--ring)) * .03); width: var(--ptr-w); height: calc((var(--s) / 2 - var(--ring)) * var(--ptr-len)); border-radius: 1px; background: var(--ptr); box-shadow: 0 0 1px rgba(0, 0, 0, .4); transform-origin: 50% calc((var(--s) / 2 - var(--ring)) * .97); transform: rotate(var(--a)); transition: transform .3s ease; }
.rs .knob::before { content: ""; position: absolute; z-index: 1; left: calc(50% - 1px); top: calc(var(--s) * .14); width: 2px; height: calc(var(--s) * .08); border-radius: 1px; background: #F4F0E8; transform-origin: 50% calc(var(--s) * .36); transform: rotate(var(--a)); transition: transform .3s ease; }
.rs .knob.blue { --tooth: #3463A2; --cap: radial-gradient(circle at 38% 30%, rgba(255, 255, 255, .2), rgba(255, 255, 255, 0) 40%), radial-gradient(circle closest-side, #24508C 0 72%, #183A69 76%, #4373B4 81%, #2F5D9B 92%, #1D4174 100%); }
.rs .knob.red { --ptr: #F4F0E8; --ptr-w: 2px; --ptr-len: .21; }
.rs .knob.red::before { content: none; }
.rs .knob.red > i { background: radial-gradient(circle at 32% 24%, rgba(255, 255, 255, .3), rgba(255, 255, 255, 0) 48%), radial-gradient(circle closest-side, #2B3442 0 82%, #171C24 84.5%, transparent 85.5%), conic-gradient(from -60deg, #D8DDE3, #8E96A0 70deg, #3A424D 160deg, #262C35 200deg, #5A626D 280deg, #D8DDE3); }
.rs .knob.red > i::before { inset: 1% 40.5%; border-radius: 3px; background: linear-gradient(90deg, transparent 43%, rgba(45, 0, 0, .55) 43% 57%, transparent 57%), linear-gradient(90deg, #5A0F0B, #A9271F 26%, #CB463C 46%, #9B221B 70%, #540E0A); -webkit-mask: none; mask: none; box-shadow: 0 0 3px rgba(0, 0, 0, .6); transform: rotate(var(--a)); transition: transform .3s ease; }
.rs .knob.red > i::after { inset: 11.5%; background: radial-gradient(circle at 36% 30%, rgba(255, 190, 170, .32), rgba(255, 190, 170, 0) 36%), radial-gradient(circle closest-side, #921C17 0 63%, #5C0F0C 66%, #C4392F 70%, #A82722 84%, #6B120E 100%); box-shadow: inset 0 -2px 3px rgba(0, 0, 0, .35), 0 2px 5px rgba(0, 0, 0, .6); }
.rs button.knob { appearance: none; margin: 0; border: 0; padding: 0; background: none; color: inherit; cursor: pointer; }
.rs button.knob:active > i { transform: scale(.96); }
.rs button.knob:active::after, .rs button.knob:active::before { transform: rotate(var(--a)) scale(.96); }
</style>
