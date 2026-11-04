<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { errorText, get } from '../api/client'
import type { Suggestion } from '../api/types'
import { fieldView, type TagRow } from '../lib/tags'
import { useEditor } from '../stores/editor'
import Icon from './Icon.vue'

const props = defineProps<{ row: TagRow }>()
const emit = defineEmits<{ close: [] }>()
const editor = useEditor()

const view = computed(() => fieldView(editor.scope, props.row.target, editor.stage))
const text = ref(view.value.values.join('\n'))
const isLyrics = computed(() => props.row.key.startsWith('LYRICS'))
const title = computed(() => (isLyrics.value ? 'Lyrics' : props.row.key))
const where = computed(() => {
  const t = editor.selected
  return t ? `Track ${String(t.track).padStart(2, '0')}, ${t.title}` : `All ${editor.scope.length} tracks`
})
const notes = computed(() => (editor.suggestions?.notes ?? []).filter((n) => props.row.fields.includes(n.field) && (!n.track || n.track === editor.trackId)))

// LRCLIB can take seconds a track, so lyrics are asked for only here.
const offers = ref<Suggestion[]>([])
const error = ref('')
onMounted(async () => {
  if (!isLyrics.value || !editor.selected) return
  try {
    const a = await get<{ suggestions: Suggestion[]; unavailable: { source: string; message: string }[] }>(`/api/library/tracks/${editor.selected.id}/suggestions`)
    offers.value = a.suggestions
    if (a.unavailable.length > 0) error.value = `${a.unavailable[0]!.source} doesn't answer.`
  } catch (err) {
    error.value = errorText(err)
  }
})
const lines = (s: Suggestion) => (s.values?.[0] ?? '').split('\n').length

function done() {
  editor.edit(props.row.target, text.value === '' ? [] : [text.value])
  emit('close')
}
</script>

<template>
  <div class="scrim" @keydown.esc.stop="emit('close')">
    <section class="win dlg" role="dialog" aria-modal="true" aria-labelledby="dl-t">
      <div class="tb"><h2 id="dl-t">{{ title }}</h2><span class="stripes"></span><button type="button" class="wbtn" aria-label="Close" @click="emit('close')"><Icon name="close" /></button></div>
      <div class="body">
        <p class="t2">{{ where }}</p>
        <label><span class="sr">{{ title }}</span><textarea v-model="text" class="lib-ta"></textarea></label>
        <p v-for="n in notes" :key="n.message" class="lf-note"><Icon name="note" /><span>{{ n.message }}</span></p>
        <p v-if="error" class="t2 sm">{{ error }}</p>
        <div v-if="offers.length > 0" class="sugs">
          <button v-for="s in offers" :key="s.source + lines(s)" type="button" class="sug" :aria-label="`Use the ${lines(s)} lines from ${s.source}, ${s.confidence}%`" @click="text = s.values?.[0] ?? ''"><span class="sug-v">{{ lines(s) }} lines</span><span class="sug-s">{{ s.source }}</span><span class="sug-p">{{ s.confidence }}%</span></button>
        </div>
      </div>
      <div class="keys"><button type="button" class="key" @click="emit('close')">Cancel</button><button type="button" class="key sun" @click="done">Done</button></div>
    </section>
  </div>
</template>

<style scoped>
.rs .scrim { position: fixed; inset: 0; z-index: 40; display: flex; align-items: center; justify-content: center; padding: 20px; background: rgba(6, 8, 12, .6); }
.rs .dlg { width: 100%; max-width: 680px; }
.rs .body { display: flex; flex-direction: column; gap: 14px; padding: 18px; }
.rs .keys { display: flex; justify-content: flex-end; gap: 12px; padding: 0 18px 18px; }
</style>
