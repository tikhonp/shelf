<script setup lang="ts">
import { usePhone } from '../lib/viewport'

// One part on the Status page: a module on the phone, a rack unit on the desktop.
defineProps<{ name: string; lamp?: string; note?: string; lines: string[]; last?: boolean }>()
const phone = usePhone()
</script>

<template>
  <section v-if="phone" class="smod" :class="{ last }" :aria-label="name">
    <div class="smod-h"><span v-if="lamp" class="led" :class="lamp"></span><h2>{{ name }}</h2><span v-if="note" class="sub">{{ note }}</span></div>
    <div v-if="lines.length" class="lcd"><div v-for="l in lines" :key="l" class="lcd-l">{{ l }}</div></div>
    <slot name="text" />
    <div v-if="$slots.keys" class="smod-keys"><slot name="keys" /></div>
    <slot />
  </section>
  <section v-else class="win unit" :aria-label="name">
    <div class="unit-body">
      <div class="plate"><h2><span v-if="lamp" class="led" :class="lamp"></span>{{ name }}</h2><span v-if="note" class="sub">{{ note }}</span></div>
      <div v-if="lines.length" class="lcd facts"><div v-for="l in lines" :key="l" class="lcd-l">{{ l }}</div></div>
      <slot name="text" />
      <div v-if="$slots.keys" class="line g12"><slot name="keys" /></div>
      <slot />
    </div>
  </section>
</template>

<style scoped>
.rs .unit { flex-direction: row; align-items: stretch; border-radius: 4px; }
.rs .unit::before, .rs .unit::after { content: ""; flex: none; width: 28px; background: radial-gradient(circle at 50% 18px, #D5D9DF 0 3.5px, #4A5059 4px 6px, transparent 6.5px), radial-gradient(circle at 50% calc(100% - 18px), #D5D9DF 0 3.5px, #4A5059 4px 6px, transparent 6.5px), var(--win-2); }
.rs .unit::before { border-right: 1px solid var(--ink); }
.rs .unit::after { border-left: 1px solid var(--ink); }
.rs .unit-body { flex: 1; min-width: 0; display: flex; flex-wrap: wrap; align-items: center; gap: 14px 24px; padding: 16px 18px; }
.rs .plate { display: flex; flex-direction: column; gap: 6px; width: 190px; flex: none; }
.rs .plate h2 { display: flex; align-items: center; gap: 10px; font-family: var(--label); font-size: 18px; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
.rs .facts { flex: 0 1 300px; min-height: 72px; justify-content: center; }
.rs .smod.last { border-bottom: 0; }
</style>
