<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { MemoryDoc } from '../api/agents'
import { useNow } from '../lib/clock'
import { ago } from '../lib/format'
import { evidenceText } from '../lib/inbox'
import { useMemory } from '../stores/memory'

const props = defineProps<{ doc: MemoryDoc }>()
const memory = useMemory()
const now = useNow()

const editing = ref(false)
const asking = ref(false)
const busy = ref(false)
const error = ref('')
const draft = ref({ title: '', scope: '', body: '', confidence: '' as string })

const statusWord = computed(() => ({ active: 'Active', proposed: 'Proposed', deleted: 'Deleted' })[props.doc.status])
const statusClass = computed(() => ({ active: 'done', proposed: 'partial', deleted: 'canceled' })[props.doc.status])
const deleteWord = computed(() => (props.doc.layer === 'preference' ? 'Delete. A later run needs new evidence to bring it back.' : 'Delete it for good.'))

function edit() {
  draft.value = { title: props.doc.title, scope: props.doc.scope, body: props.doc.body, confidence: props.doc.confidence == null ? '' : String(props.doc.confidence) }
  error.value = ''
  editing.value = true
}

async function run(fn: () => ReturnType<typeof memory.confirm>, after?: () => void) {
  busy.value = true
  error.value = ''
  const r = await fn()
  busy.value = false
  if (r.ok) after?.()
  else error.value = r.error ?? 'Not saved'
}

// A stale save keeps the form open with the reviewer's text; the version under it is now the current one.
const save = () =>
  run(
    () =>
      memory.correct(props.doc, {
        title: draft.value.title,
        scope: draft.value.scope,
        body: draft.value.body,
        ...(props.doc.layer === 'rule' ? {} : { confidence: draft.value.confidence === '' ? null : Math.max(0, Math.min(100, Number(draft.value.confidence))) }),
      }),
    () => (editing.value = false),
  )
const confirmRule = () => run(() => memory.confirm(props.doc))
const remove = () => run(() => memory.remove(props.doc), () => (asking.value = false))
</script>

<template>
  <article class="mem" :class="doc.status" :aria-labelledby="`mem-${doc.id}`">
    <div class="line g8 wrap">
      <h3 :id="`mem-${doc.id}`" class="grow">{{ doc.title }}</h3>
      <span class="pill" :class="statusClass">{{ statusWord }}</span>
    </div>
    <p class="t2 sm"><span class="mono">{{ doc.key }}</span> · version {{ doc.version }} · {{ doc.author_kind === 'agent' ? 'Agent' : 'User' }} {{ doc.author }} · {{ ago(doc.updated_at, now) }}</p>
    <p v-if="doc.corrected_event" class="t2 sm">A user corrected or deleted this (event {{ doc.corrected_event }}). An agent needs newer evidence to change it.</p>
    <template v-if="!editing">
      <p v-if="doc.scope" class="sm"><span class="bold">Scope:</span> {{ doc.scope }}</p>
      <p class="body">{{ doc.body }}</p>
      <p v-if="doc.confidence != null" class="t2 sm">Confidence {{ doc.confidence }}%</p>
      <div v-if="doc.evidence.length" class="col g4">
        <span class="lbl-f">Why it exists</span>
        <ul class="ev">
          <li v-for="(e, i) in doc.evidence" :key="i" class="sm">
            <RouterLink v-if="e.proposal != null" :to="`/inbox/${e.proposal}`">{{ evidenceText(e) }}</RouterLink>
            <span v-else>{{ evidenceText(e) }}</span>
          </li>
        </ul>
      </div>
    </template>
    <form v-else class="col g8" @submit.prevent="save">
      <label class="lbl-f">Title<input v-model="draft.title" class="lib-in" maxlength="200" required /></label>
      <label class="lbl-f">Scope<input v-model="draft.scope" class="lib-in" maxlength="500" /></label>
      <label class="lbl-f">Text<textarea v-model="draft.body" class="ib-text" rows="6" required></textarea></label>
      <label v-if="doc.layer !== 'rule'" class="lbl-f">Confidence, 0 to 100<input v-model="draft.confidence" class="lib-in num" type="number" min="0" max="100" /></label>
      <div class="line g8 wrap">
        <button type="submit" class="key sm sun" :disabled="busy">Save correction</button>
        <button type="button" class="key sm" :disabled="busy" @click="editing = false">Cancel</button>
      </div>
    </form>
    <p v-if="error" class="bad-text sm" role="alert">{{ error }}</p>
    <div v-if="asking" class="note ask" role="alertdialog" :aria-label="`Delete ${doc.title}`">
      <span class="grow sm">{{ deleteWord }}</span>
      <button type="button" class="key sm" @click="asking = false">Cancel</button>
      <button type="button" class="key sm sun" :disabled="busy" @click="remove">Delete</button>
    </div>
    <div v-else-if="doc.status !== 'deleted' && !editing" class="line g8 wrap">
      <button v-if="doc.status === 'proposed'" type="button" class="key sm sun" :disabled="busy" @click="confirmRule">Confirm rule</button>
      <button type="button" class="key sm" :disabled="busy" @click="edit">Correct</button>
      <button type="button" class="key sm" :disabled="busy" @click="asking = true">Delete</button>
    </div>
  </article>
</template>

<style scoped>
.rs .mem { display: flex; flex-direction: column; gap: 8px; padding: 14px; border-bottom: 1px solid var(--line); }
.rs .mem.deleted { opacity: .8; }
.rs .mem h3 { font-size: 16px; font-weight: 600; }
.rs .body { white-space: pre-wrap; overflow-wrap: anywhere; }
.rs .ev { display: flex; flex-direction: column; gap: 2px; margin: 0; padding-left: 18px; }
.rs .ask { align-items: center; flex-wrap: wrap; }
</style>
