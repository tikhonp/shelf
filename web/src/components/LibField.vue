<script setup lang="ts">
import { computed } from 'vue'
import type { Suggestion } from '../api/types'
import { fieldNote } from '../lib/notes'
import { fieldView, isLong, offersFor, type MainField } from '../lib/tags'
import { useEditor } from '../stores/editor'
import Icon from './Icon.vue'
import LibTip from './LibTip.vue'
import LibSug from './LibSug.vue'

const props = defineProps<{ f: MainField }>()
defineEmits<{ long: [] }>()
const editor = useEditor()

const target = computed(() => props.f.field)
const view = computed(() => fieldView(editor.scope, target.value, editor.stage))
const locked = computed(() => !!props.f.perTrack && !editor.selected)
const readOnly = computed(() => locked.value || view.value.readOnly)
const id = computed(() => `lf-${props.f.field}`)
const placeholder = computed(() => (view.value.mixed ? 'Mixed' : 'Empty'))
const single = computed(() => view.value.values[0] ?? '')
const was = computed(() => {
  const w = view.value.was
  if (!w) return ''
  if (w.mixed) return 'Was mixed'
  return w.values.length ? `Was ${w.values.join(', ')}` : 'Was empty'
})

const offers = computed<Suggestion[]>(() => offersFor(editor.suggestions, target.value, editor.trackId, locked.value, view.value.values))
const notes = computed(() => (editor.suggestions?.notes ?? []).filter((n) => n.field === target.value && (!n.track || n.track === editor.trackId)))

const rows = computed(() => (view.value.mixed ? [''] : view.value.values.length ? view.value.values : ['']))

function type(e: Event) {
  const v = (e.target as HTMLInputElement).value
  editor.edit(target.value, v === '' ? [] : [v])
}
function typeAt(i: number, e: Event) {
  const v = (e.target as HTMLInputElement).value
  if (view.value.mixed) return editor.edit(target.value, v === '' ? [] : [v])
  const next = [...view.value.values]
  next[i] = v
  editor.edit(target.value, next)
}
function removeAt(i: number) {
  editor.edit(target.value, view.value.values.filter((_, n) => n !== i))
}
function addRow() {
  editor.edit(target.value, [...view.value.values, ''])
}
const on = computed(() => view.value.values[0] === '1')
</script>

<template>
  <div class="lf" :class="{ cursor: editor.cursor === target }" :data-target="target" @focusin="editor.cursor = target">
    <label class="lf-l" :for="id"><span v-if="view.staged" class="led stg" role="img" aria-label="Staged"></span><LibTip :title="f.label" :note="fieldNote(f.field)" :open="editor.noteOn === target">{{ f.label }}</LibTip></label>
    <div class="lf-c">
      <div v-if="f.flag" class="lf-flag">
        <button :id="id" type="button" class="rocker flat" :class="{ mixed: view.mixed }" role="switch" :aria-checked="on" :disabled="readOnly" @click="editor.edit(target, on ? [] : ['1'])">
          <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span><span class="t2">{{ view.mixed ? 'Mixed' : on ? 'On' : 'Off' }}</span>
        </button>
      </div>
      <span v-else-if="isLong(view.values)" class="lf-in">
        <button :id="id" type="button" class="lib-in long" :class="{ stg: view.staged, ro: readOnly }" :disabled="readOnly" @click="$emit('long')"><span class="ell">{{ view.values[0] }}</span></button>
      </span>
      <template v-else-if="f.multi && !readOnly">
        <span v-for="(v, i) in rows" :key="i" class="lf-in">
          <input :id="i === 0 ? id : undefined" class="lib-in" :class="{ mixed: view.mixed, stg: view.staged }" :value="v" :placeholder="placeholder" :aria-label="`${f.label}, value ${i + 1}`" @input="typeAt(i, $event)" />
          <button v-if="rows.length > 1" type="button" class="lf-rev" :aria-label="`Remove ${f.label} value ${i + 1}`" title="Remove" @click="removeAt(i)"><Icon name="close" /></button>
          <button v-else-if="view.staged" type="button" class="lf-rev" :aria-label="`Revert ${f.label}`" title="Revert" @click="editor.drop(target)"><Icon name="revert" /></button>
        </span>
        <span class="lf-adds">
          <button v-if="!view.mixed" type="button" class="lf-add" @click="addRow"><Icon name="plus" size="s" />Add value</button>
          <button v-if="view.staged && rows.length > 1" type="button" class="lf-add" @click="editor.drop(target)"><Icon name="revert" size="s" />Revert</button>
        </span>
      </template>
      <span v-else class="lf-in">
        <input :id="id" class="lib-in" :class="{ mixed: view.mixed && !readOnly, stg: view.staged, ro: readOnly }" :value="single" :placeholder="placeholder" :readonly="readOnly" @input="type" />
        <span v-if="readOnly" class="lf-lock" aria-hidden="true"><Icon name="lock" /></span>
        <button v-else-if="view.staged" type="button" class="lf-rev" :aria-label="`Revert ${f.label}`" title="Revert" @click="editor.drop(target)"><Icon name="revert" /></button>
      </span>
      <span v-if="was" class="lf-was">{{ was }}</span>
      <p v-if="view.readOnly" class="lf-note"><Icon name="note" /><span>shelf can't change this tag in this file.</span></p>
      <p v-for="n in notes" :key="n.message" class="lf-note"><Icon name="note" /><span>{{ n.message }}</span></p>
    </div>
    <div class="lf-s"><LibSug v-for="s in offers" :key="`${s.source}${s.values?.join()}`" :s="s" @use="editor.take(s, !!f.multi)" /></div>
  </div>
</template>

<style scoped>
.rs .long { display: flex; align-items: center; text-align: left; cursor: pointer; }
.rs .long.ro { cursor: default; }
</style>
