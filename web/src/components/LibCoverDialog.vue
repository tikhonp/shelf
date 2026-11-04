<script setup lang="ts">
import { computed, ref } from 'vue'
import { errorText, post, upload } from '../api/client'
import type { CoverUpload } from '../api/types'
import { useEditor } from '../stores/editor'
import Cover from './Cover.vue'
import Icon from './Icon.vue'

const emit = defineEmits<{ close: [] }>()
const editor = useEditor()

// choice is what the dialog previews: the current cover or a new one.
interface Choice {
  id: string
  label: string
  detail: string
  preview: string
  // get makes the choice an upload a Save can name.
  get?: () => Promise<CoverUpload>
}

const uploaded = ref<Choice[]>([])
const picked = ref('current')
const busy = ref(false)
const error = ref('')
const file = ref<HTMLInputElement>()

const damaged = computed(() => editor.album?.issues.some((i) => i.kind === 'bad_cover') ?? false)
const noCover = computed(() => editor.album?.issues.some((i) => i.kind === 'no_cover') ?? false)
const choices = computed<Choice[]>(() => [
  { id: 'current', label: noCover.value ? 'No cover' : 'Current cover', detail: '', preview: editor.coverUrl },
  ...(editor.suggestions?.covers ?? []).map<Choice>((c) => ({
    id: c.image,
    label: c.source,
    detail: `${c.confidence}%`,
    preview: `/api/library/remote/${c.image}`,
    get: () => post<CoverUpload>(`/api/library/remote/${c.image}/cover`),
  })),
  ...uploaded.value,
])
const choice = computed(() => choices.value.find((c) => c.id === picked.value) ?? choices.value[0]!)
const isNew = computed(() => choice.value.id !== 'current')

function add(u: CoverUpload, label: string) {
  uploaded.value = [...uploaded.value, { id: u.id, label, detail: `${u.width} x ${u.height}`, preview: `/api/library/covers/${u.id}`, get: async () => u }]
  picked.value = u.id
}

async function run(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await fn()
  } catch (err) {
    error.value = errorText(err)
  } finally {
    busy.value = false
  }
}

const chosen = () => run(async () => {
  const f = file.value?.files?.[0]
  if (f) add(await upload<CoverUpload>('/api/library/covers', f), f.name)
})
const repair = () => run(async () => add(await post<CoverUpload>(`/api/library/albums/${editor.album?.id}/cover/repair`), 'Repaired cover'))

const apply = () => run(async () => {
  const u = await choice.value.get!()
  editor.stageCover({ upload: u.id, preview: choice.value.preview })
  emit('close')
})
const alone = () => run(async () => {
  const u = await choice.value.get!()
  await editor.saveCoverAlone(u.id)
  emit('close')
})
function remove() {
  editor.stageCover({ delete: true })
  emit('close')
}
</script>

<template>
  <div class="scrim">
    <section class="win dlg" role="dialog" aria-modal="true" aria-labelledby="dc-t">
      <div class="tb"><h2 id="dc-t">Cover</h2><span class="stripes"></span><button type="button" class="wbtn" aria-label="Close" @click="emit('close')"><Icon name="close" /></button></div>
      <div class="body">
        <Cover :url="choice.preview" :size="300" big />
        <div class="col g8 list">
          <button v-for="c in choices" :key="c.id" type="button" class="lib-thumb" :aria-pressed="c.id === picked" @click="picked = c.id">
            <Cover :url="c.preview" :size="56" />
            <span class="two"><span class="ttl small">{{ c.label }}</span><span v-if="c.detail" class="sub num">{{ c.detail }}</span></span>
          </button>
          <input ref="file" class="sr" type="file" accept="image/jpeg,image/png" aria-label="Upload a cover" @change="chosen" />
          <button type="button" class="key block" :disabled="busy" @click="file?.click()"><Icon name="upload" size="s" />Upload</button>
          <button v-if="damaged" type="button" class="key block" :disabled="busy" @click="repair">Repair</button>
        </div>
      </div>
      <p v-if="error" class="bad-text note-line" role="alert">{{ error }}</p>
      <p class="t2 note-line">A cover edit changes the embedded cover and the folder's cover file.</p>
      <div class="keys">
        <button type="button" class="key" :disabled="busy" @click="remove">Delete</button>
        <span class="line g12">
          <button type="button" class="key" :disabled="busy || !isNew" @click="alone">Save cover alone</button>
          <button type="button" class="key sun" :disabled="busy || !isNew" @click="apply">Apply to album</button>
        </span>
      </div>
    </section>
  </div>
</template>

<style scoped>
.rs .scrim { position: fixed; inset: 0; z-index: 40; display: flex; align-items: center; justify-content: center; padding: 20px; background: rgba(6, 8, 12, .6); }
.rs .dlg { width: 100%; max-width: 720px; max-height: 100%; overflow-y: auto; }
.rs .body { display: flex; flex-wrap: wrap; gap: 22px; padding: 18px; }
.rs .list { flex: 1 1 240px; min-width: 0; }
.rs .small { font-size: 14px; }
.rs .note-line { padding: 0 18px 14px; }
.rs .keys { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 12px; padding: 0 18px 18px; }
@media (max-width: 719px) {
  .rs .scrim { padding: 8px; }
  .rs .keys .line { width: 100%; }
  .rs .keys .line .key { flex: 1; }
}
</style>
