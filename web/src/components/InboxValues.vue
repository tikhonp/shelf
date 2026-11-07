<script setup lang="ts">
// One tag's values, one input per value. A value is never split on commas or semicolons.
const props = defineProps<{ values: string[]; label: string; long?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ update: [values: string[]] }>()

function change(i: number, v: string) {
  emit('update', props.values.map((x, j) => (j === i ? v : x)))
}
const remove = (i: number) => emit('update', props.values.filter((_, j) => j !== i))
const add = () => emit('update', [...props.values, ''])
</script>

<template>
  <div class="col g4" role="group" :aria-label="label">
    <div v-for="(v, i) in values" :key="i" class="line g8">
      <textarea v-if="long" class="ib-text grow" rows="5" :value="v" :disabled="disabled" :aria-label="`${label}, value ${i + 1}`" @input="change(i, ($event.target as HTMLTextAreaElement).value)"></textarea>
      <input v-else class="lib-in grow" :value="v" :disabled="disabled" :aria-label="`${label}, value ${i + 1}`" @input="change(i, ($event.target as HTMLInputElement).value)" />
      <button v-if="!disabled" type="button" class="key sm icon" :aria-label="`Remove ${label} value ${i + 1}`" title="Remove value" @click="remove(i)">×</button>
    </div>
    <p v-if="values.length === 0" class="t2 sm">No values: the tag is removed.</p>
    <button v-if="!disabled" type="button" class="lf-add" @click="add">Add value</button>
  </div>
</template>
