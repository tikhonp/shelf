<script setup lang="ts">
import { nextTick, ref } from 'vue'

// One tag's values, one input per value. A value is never split on commas or semicolons.
const props = defineProps<{ values: string[]; label: string; long?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ update: [values: string[]] }>()
const root = ref<HTMLElement>()

function change(i: number, v: string) {
  emit('update', props.values.map((x, j) => (j === i ? v : x)))
}
function remove(i: number) {
  emit('update', props.values.filter((_, j) => j !== i))
  void nextTick(() => {
    const inputs = root.value?.querySelectorAll<HTMLElement>('input, textarea')
    if (inputs && inputs.length > 0) inputs[Math.min(i, inputs.length - 1)]?.focus()
    else root.value?.querySelector<HTMLElement>('.lf-add')?.focus()
  })
}
function add() {
  const nextIndex = props.values.length
  emit('update', [...props.values, ''])
  void nextTick(() => {
    const inputs = root.value?.querySelectorAll<HTMLElement>('input, textarea')
    ;(inputs?.[nextIndex] ?? inputs?.[inputs.length - 1])?.focus()
  })
}
</script>

<template>
  <div ref="root" class="col g4" role="group" :aria-label="label">
    <div v-for="(v, i) in values" :key="i" class="line g8">
      <textarea v-if="long" class="ib-text grow" rows="5" :value="v" :disabled="disabled" :aria-label="`${label}, value ${i + 1}`" @input="change(i, ($event.target as HTMLTextAreaElement).value)"></textarea>
      <input v-else class="lib-in grow" :value="v" :disabled="disabled" :aria-label="`${label}, value ${i + 1}`" @input="change(i, ($event.target as HTMLInputElement).value)" />
      <button v-if="!disabled" type="button" class="key sm icon" :aria-label="`Remove ${label} value ${i + 1}`" title="Remove value" @click="remove(i)">×</button>
    </div>
    <p v-if="values.length === 0" class="t2 sm">No values: the tag is removed.</p>
    <button v-if="!disabled" type="button" class="lf-add" @click="add">Add value</button>
  </div>
</template>
