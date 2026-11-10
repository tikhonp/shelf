<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import type { MemoryLayer } from '../api/agents'
import Icon from '../components/Icon.vue'
import MemoryDoc from '../components/MemoryDoc.vue'
import { usePhone } from '../lib/viewport'
import { useInbox } from '../stores/inbox'
import { useMemory, type StatusFilter } from '../stores/memory'

const router = useRouter()
const phone = usePhone()
const inbox = useInbox()
const memory = useMemory()

const layers: { id: MemoryLayer; label: string; about: string }[] = [
  { id: 'rule', label: 'Rules', about: 'Rules users set. An agent can only propose one; a user confirms it.' },
  { id: 'preference', label: 'Preferences', about: 'What agents learned from reviews and edits. They guide new proposals at once; correct or delete any of them.' },
  { id: 'context', label: 'Context', about: 'Notes, research and checkpoints agents keep between runs.' },
]
const statuses: { id: StatusFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'active', label: 'Active' },
  { id: 'proposed', label: 'Proposed' },
  { id: 'deleted', label: 'Deleted' },
]

const adding = ref(false)
const rule = ref({ key: '', title: '', scope: '', body: '' })
const addError = ref('')
const resetting = ref(false)
const resetLayers = ref<MemoryLayer[]>([])
const resetError = ref('')

watch(() => inbox.enabled, (on) => { if (on === false) void router.replace('/library') })
onMounted(async () => {
  if (inbox.enabled === null) await inbox.detect()
  if (inbox.enabled) void memory.load()
})

async function addRule() {
  addError.value = ''
  const r = await memory.create(rule.value)
  if (r.ok) {
    rule.value = { key: '', title: '', scope: '', body: '' }
    adding.value = false
  } else addError.value = r.error ?? 'Not saved'
}

async function reset() {
  resetError.value = ''
  const r = await memory.reset(resetLayers.value)
  if (r.ok) {
    resetting.value = false
    resetLayers.value = []
  } else resetError.value = r.error ?? 'Not reset'
}
const toggleLayer = (l: MemoryLayer) => (resetLayers.value = resetLayers.value.includes(l) ? resetLayers.value.filter((x) => x !== l) : [...resetLayers.value, l])
</script>

<template>
  <div :class="phone ? 'stack' : 'page'">
    <section class="win mem-win" aria-labelledby="mem-h">
      <div class="tb">
        <RouterLink class="wbtn" to="/inbox" aria-label="Back to the inbox" title="Inbox"><Icon name="back" /></RouterLink>
        <h1 id="mem-h">Memory</h1>
        <span class="stripes"></span>
        <a class="wbtn txt" href="/api/inbox/memory/export" download>Export</a>
        <button type="button" class="wbtn txt" @click="resetting = !resetting">Reset memory</button>
      </div>
      <div class="vscroll">
        <div class="col g10 pad bar">
          <div class="segk block" role="radiogroup" aria-label="Layer">
            <button v-for="l in layers" :key="l.id" type="button" role="radio" :aria-checked="memory.layer === l.id" @click="memory.show(l.id)">{{ l.label }}</button>
          </div>
          <p class="t2 sm">{{ layers.find((l) => l.id === memory.layer)?.about }} Memory changes no tags or files, and no rule lets an agent skip review.</p>
          <div class="segk" role="radiogroup" aria-label="Status">
            <button v-for="s in statuses" :key="s.id" type="button" role="radio" :aria-checked="memory.status === s.id" @click="memory.show(memory.layer, s.id)">{{ s.label }}</button>
          </div>
          <div class="line g8 wrap">
            <button v-if="memory.layer === 'rule' && !adding" type="button" class="key sm" @click="adding = true"><Icon name="plus" size="s" />New rule</button>
          </div>
        </div>
        <form v-if="adding" class="col g8 pad bar" aria-label="New rule" @submit.prevent="addRule">
          <label class="lbl-f">Key<input v-model="rule.key" class="lib-in mono" maxlength="120" placeholder="dates.original" required /></label>
          <label class="lbl-f">Title<input v-model="rule.title" class="lib-in" maxlength="200" required /></label>
          <label class="lbl-f">Scope<input v-model="rule.scope" class="lib-in" maxlength="500" placeholder="All albums" /></label>
          <label class="lbl-f">Rule<textarea v-model="rule.body" class="ib-text" rows="5" required></textarea></label>
          <p v-if="addError" class="bad-text sm" role="alert">{{ addError }}</p>
          <div class="line g8"><button type="submit" class="key sm sun">Add rule</button><button type="button" class="key sm" @click="adding = false">Cancel</button></div>
        </form>
        <div v-if="resetting" class="note ask" role="alertdialog" aria-label="Reset memory">
          <div class="col g8 grow">
            <span class="bold">Reset which layers?</span>
            <span class="sub">Rules and context are removed. Preferences become deleted entries, so old feedback can't bring them back. Download an export first to keep a copy.</span>
            <div class="line g12 wrap">
              <label v-for="l in layers" :key="l.id" class="ib-check"><input type="checkbox" :checked="resetLayers.includes(l.id)" @change="toggleLayer(l.id)" />{{ l.label }}</label>
            </div>
            <p v-if="resetError" class="bad-text sm" role="alert">{{ resetError }}</p>
          </div>
          <button type="button" class="key sm" @click="resetting = false">Cancel</button>
          <button type="button" class="key sm sun" :disabled="resetLayers.length === 0" @click="reset">Reset</button>
        </div>
        <p v-if="memory.error" class="bad-text pad" role="alert">{{ memory.error }}</p>
        <div v-if="!memory.loading && memory.docs.length === 0 && !memory.error" class="empty"><h2>Nothing here</h2></div>
        <MemoryDoc v-for="d in memory.docs" :key="d.id" :doc="d" />
        <div v-if="memory.next !== null" class="pad"><button type="button" class="key" :disabled="memory.loading" @click="memory.more()">Load more</button></div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.rs .pad { padding: 12px 14px; }
.rs .bar { border-bottom: 1px solid var(--ink); }
.rs .ask { margin: 12px 14px; align-items: flex-end; flex-wrap: wrap; }
.rs .page > .mem-win { flex: 1; max-width: 1000px; }
</style>
