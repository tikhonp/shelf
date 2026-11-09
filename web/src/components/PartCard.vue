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
.rs .facts { flex: 0 1 300px; min-height: 72px; justify-content: center; }
.rs .smod.last { border-bottom: 0; }
</style>
