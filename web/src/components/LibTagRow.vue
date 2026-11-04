<script setup lang="ts">
import { computed } from 'vue'
import { fieldView, isLong as tooLong, pastedRelease, suggestionsFor, type TagRow } from '../lib/tags'
import { useEditor } from '../stores/editor'
import Icon from './Icon.vue'
import LibSug from './LibSug.vue'

const props = defineProps<{ row: TagRow }>()
defineEmits<{ long: [] }>()
const editor = useEditor()

const view = computed(() => fieldView(editor.scope, props.row.target, editor.stage))
const locked = computed(() => props.row.perTrack && !editor.selected)
const readOnly = computed(() => locked.value || view.value.readOnly)
const id = computed(() => `tag-${props.row.key}`)
const placeholder = computed(() => (view.value.mixed ? 'Mixed' : 'Empty'))
const rows = computed(() => (view.value.mixed ? [''] : view.value.values.length ? view.value.values : ['']))
const gone = computed(() => view.value.staged && view.value.values.length === 0 && !view.value.mixed)
const held = computed(() => view.value.was?.values.join(', ') ?? '')
const isLong = computed(() => props.row.key.startsWith('LYRICS') || tooLong(view.value.values))
const was = computed(() => {
  const w = view.value.was
  if (!w || gone.value) return ''
  if (w.mixed) return 'Was mixed'
  return w.values.length ? `Was ${w.values.join(', ')}` : 'Was empty'
})
const offers = computed(() => {
  const now = view.value.values.join('\u0000')
  return props.row.fields
    .flatMap((f) => suggestionsFor(editor.suggestions, f, editor.trackId))
    .filter((s) => s.tracks || (!locked.value && (s.values ?? []).join('\u0000') !== now))
    .slice(0, 3)
})

function type(i: number, e: Event) {
  const v = (e.target as HTMLInputElement).value
  // Pasting a release into the album ID field moves every release field to it.
  const release = props.row.fields.includes('musicbrainz_albumid') ? pastedRelease(v) : null
  if (release) void editor.loadSuggestions(release)
  if (view.value.mixed || !props.row.multi) return editor.edit(props.row.target, v === '' ? [] : [v])
  const next = [...view.value.values]
  next[i] = v
  editor.edit(props.row.target, next)
}
const removeAt = (i: number) => editor.edit(props.row.target, view.value.values.filter((_, n) => n !== i))
</script>

<template>
  <div class="lf tagrow" :class="{ cursor: editor.cursor === row.target }" @focusin="editor.cursor = row.target">
    <label class="lf-l tag" :for="id"><span v-if="view.staged" class="led stg" role="img" aria-label="Staged"></span><span>{{ row.key }}</span></label>
    <div class="lf-c">
      <span v-if="gone" class="lf-in">
        <input :id="id" class="lib-in stg struck" :value="held" readonly />
        <button type="button" class="lf-rev" :aria-label="`Revert ${row.key}`" title="Revert" @click="editor.drop(row.target)"><Icon name="revert" /></button>
      </span>
      <span v-else-if="isLong" class="lf-in">
        <button :id="id" type="button" class="lib-in long" :class="{ stg: view.staged, ro: locked }" :disabled="locked" @click="$emit('long')"><span class="ell">{{ view.mixed ? 'Mixed' : view.values[0] || 'Empty' }}</span></button>
      </span>
      <template v-else>
        <span v-for="(v, i) in rows" :key="i" class="lf-in">
          <input :id="i === 0 ? id : undefined" class="lib-in" :class="{ mixed: view.mixed && !readOnly, stg: view.staged, ro: readOnly }" :value="v" :placeholder="placeholder" :readonly="readOnly" :aria-label="row.multi ? `${row.key}, value ${i + 1}` : undefined" @input="type(i, $event)" />
          <span v-if="readOnly" class="lf-lock" aria-hidden="true"><Icon name="lock" /></span>
          <button v-else-if="rows.length > 1" type="button" class="lf-rev" :aria-label="`Remove ${row.key} value ${i + 1}`" title="Remove" @click="removeAt(i)"><Icon name="close" /></button>
          <button v-else-if="view.staged" type="button" class="lf-rev" :aria-label="`Revert ${row.key}`" title="Revert" @click="editor.drop(row.target)"><Icon name="revert" /></button>
          <button v-else-if="row.group === 'extended' && view.values.length > 0" type="button" class="lf-rev" :aria-label="`Delete ${row.key}`" title="Delete" @click="editor.edit(row.target, [])"><Icon name="close" /></button>
        </span>
        <span v-if="row.multi && !readOnly && !view.mixed" class="lf-adds"><button type="button" class="lf-add" @click="editor.edit(row.target, [...view.values, ''])"><Icon name="plus" size="s" />Add value</button></span>
      </template>
      <span v-if="was" class="lf-was">{{ was }}</span>
    </div>
    <div v-if="offers.length > 0" class="lf-s"><LibSug v-for="s in offers" :key="`${s.field}${s.source}${s.values?.join()}`" :s="s" @use="editor.take(s, row.multi)" /></div>
  </div>
</template>

<style scoped>
.rs .long { display: flex; align-items: center; text-align: left; cursor: pointer; }
.rs .long.ro { cursor: default; }
</style>
