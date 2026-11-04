<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { fieldNote } from '../lib/notes'
import { fieldView, mainFields, suggestionsFor } from '../lib/tags'
import { useEditor } from '../stores/editor'
import { useHelp } from '../stores/help'
import Icon from './Icon.vue'
import LibScope from './LibScope.vue'
import LibSug from './LibSug.vue'

const props = defineProps<{ field: string }>()
const editor = useEditor()
const help = useHelp()
const router = useRouter()
const note = computed(() => fieldNote(props.field))

const f = computed(() => mainFields.find((m) => m.field === props.field) ?? { field: props.field, label: props.field })
const view = computed(() => fieldView(editor.scope, props.field, editor.stage))
const offers = computed(() => suggestionsFor(editor.suggestions, props.field, editor.trackId).slice(0, 3))
const rows = computed(() => (view.value.mixed ? [''] : view.value.values.length ? view.value.values : ['']))
const multi = computed(() => 'multi' in f.value && !!f.value.multi)
const was = computed(() => {
  const w = view.value.was
  if (!w) return ''
  if (w.mixed) return 'Was mixed'
  return w.values.length ? `Was ${w.values.join(', ')}` : 'Was empty'
})

function type(i: number, e: Event) {
  const v = (e.target as HTMLInputElement).value
  if (view.value.mixed || !multi.value) return editor.edit(props.field, v === '' ? [] : [v])
  const next = [...view.value.values]
  next[i] = v
  editor.edit(props.field, next)
}
</script>

<template>
  <section class="win" aria-labelledby="lf-t">
    <div class="tb">
      <button type="button" class="wbtn" aria-label="Back to the album" @click="router.push({ query: {} })"><Icon name="back" /></button>
      <h1 id="lf-t">{{ f.label }}</h1>
      <span class="stripes"></span>
      <button v-if="note" type="button" class="wbtn q" aria-label="Note on this page" :aria-pressed="!!help.note" @click="help.note = help.note ? null : { title: f.label, text: note }">?</button>
    </div>
    <div v-if="editor.album" class="vscroll page">
      <div class="scope"><LibScope /></div>
      <div class="col g8 pad">
        <label v-for="(v, i) in rows" :key="i" class="lbl-f">{{ rows.length > 1 ? `${f.label} ${i + 1}` : f.label }}<input class="input" :value="v" :placeholder="view.mixed ? 'Mixed' : 'Empty'" @input="type(i, $event)" /></label>
        <span v-if="was" class="lf-was">{{ was }}</span>
        <span class="lf-adds">
          <button v-if="multi && !view.mixed" type="button" class="lf-add" @click="editor.edit(field, [...view.values, ''])"><Icon name="plus" size="s" />Add value</button>
          <button v-if="view.staged" type="button" class="lf-add" @click="editor.drop(field)"><Icon name="revert" size="s" />Revert</button>
        </span>
      </div>
      <div class="col g8 pad"><LibSug v-for="s in offers" :key="`${s.source}${s.values?.join()}`" :s="s" block @use="editor.take(s, multi)" /></div>
    </div>
  </section>
</template>

<style scoped>
.rs .page { gap: 14px; padding: 14px 0 12px; }
.rs .pad { padding: 0 12px; }
.rs .scope { padding: 0 12px; }
.rs .scope :deep(.lib-scope) { position: relative; top: 0; margin: 0; }
</style>
