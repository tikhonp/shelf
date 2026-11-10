<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { Evidence, Refusal } from '../api/agents'
import { useNow } from '../lib/clock'
import { ago, count } from '../lib/format'
import { eventLine, opName, outcomeClass, outcomeWord, stateClass, stateWord } from '../lib/inbox'
import { inboxNotes, reviewNote } from '../lib/notes'
import { fieldLabel } from '../lib/tags'
import { useHelp } from '../stores/help'
import { ordered, useInbox } from '../stores/inbox'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import InboxBar from './InboxBar.vue'
import InboxCable, { type CablePair } from './InboxCable.vue'
import InboxDiff from './InboxDiff.vue'
import InboxOp from './InboxOp.vue'
import LibTip from './LibTip.vue'

const props = defineProps<{ phone?: boolean }>()
defineEmits<{ close: [] }>()
const inbox = useInbox()
const help = useHelp()
// A note on the phone opens from the ? cap, so a tap never opens one under the finger.
const tip = (note: string) => (props.phone ? '' : note)
const now = useNow()

const d = computed(() => inbox.detail)
const reply = ref('')
const asking = ref(false)
const bank = ref<HTMLElement>()

const albumId = computed(() => d.value?.album.current_id ?? 0)
const cover = computed(() => (albumId.value ? `/api/library/albums/${albumId.value}/cover` : ''))
const head = computed(() => d.value?.revisions.find((r) => r.revision === d.value?.head))
const hasPending = computed(() => (d.value?.summary.pending ?? 0) > 0 && d.value?.state !== 'withdrawn')
const ops = computed(() => (d.value ? ordered(d.value.current.ops) : []))
const name = (id: string) => opName(d.value?.current.ops ?? [], id)
const superseded = computed(() => d.value?.ops.filter((o) => !d.value!.current.ops.some((c) => c.id === o.id)) ?? [])
// pairs are the fields requires links, each drawn as one patch cable.
const pairs = computed<CablePair[]>(() => ops.value.flatMap((op) => (op.requires ?? [])
  .filter((req) => ops.value.some((o) => o.id === req))
  .map((req) => ({ from: req, to: op.id, label: `${name(req)} and ${name(op.id)} go together` }))))
const byLine = computed(() => {
  const v = d.value
  if (!v) return ''
  const when = head.value ? ` · ${ago(head.value.created_at, now.value)}` : ''
  return `${v.connection.name}${when} · `
})
const editedBy = computed(() => (head.value && head.value.author !== d.value?.connection.name ? `, edited by ${head.value.author}` : ''))
const cableNote = computed(() => [...pairs.value.map((p) => `${p.label}.`), inboxNotes.cable].join(' '))
function toggleNote() {
  help.note = help.note ? null : { title: 'Review', text: reviewNote(d.value?.current.confidence, pairs.value.length > 0) }
}

// The diff the reviewer accepts is the one for the selection; with nothing selected the page shows every pending field.
const diff = computed(() => {
  const v = d.value
  if (!v) return null
  const s = inbox.selection
  if (inbox.selected.length > 0 && s?.preview) return { preview: s.preview, title: 'What Accept writes', confirm: true }
  if (v.live?.preview && hasPending.value) return { preview: v.live.preview, title: 'All pending fields', note: 'Against the files now', confirm: false }
  return { preview: v.current.preview, title: 'As proposed', note: 'Recorded with the revision', confirm: false }
})
const writes = computed(() => inbox.writes)

// The server names op IDs in its refusals; the page names the fields instead.
function refusalText(r: Refusal): string {
  const m = /^operation (\S+) requires (\S+)$/.exec(r.error)
  if (m) return `${name(m[1]!)} goes with ${name(m[2]!)}, which is no longer pending, so it can't be accepted.`
  return r.error
}
const refusal = computed(() => {
  const r = (inbox.selected.length > 0 ? inbox.selection?.refusal : d.value?.live?.refusal) ?? null
  return r ? refusalText(r) : ''
})
const cascadeText = computed(() => {
  const c = inbox.cascade
  if (!c) return ''
  const moved = c.moved.map(name).join(', ')
  return c.on ? `${name(c.op)} needs ${moved}, so ${c.moved.length === 1 ? 'it' : 'they'} switched on too.` : `${moved} ${c.moved.length === 1 ? 'goes' : 'go'} with ${name(c.op)}, so ${c.moved.length === 1 ? 'it' : 'they'} switched off too.`
})

// Evidence is tape like a suggestion: the value, then where it came from. Only a web address opens.
const kinds: Record<string, string> = { suggestion: 'Suggestion', memory: 'Memory', change: 'Change', event: 'Event', note: 'Note', link: 'Link' }
// A field is named in the editor's words; a raw key stays as the file holds it.
const tapeValue = (e: Evidence) => e.text || e.url || ([typeof e.field === 'string' && e.field ? fieldLabel(e.field) : '', e.key, e.anchor].filter((x) => typeof x === 'string' && x).join(' · ') || kinds[e.kind] || e.kind)
const tapeSource = (e: Evidence) => (typeof e.source === 'string' && e.source ? e.source : kinds[e.kind] ?? e.kind)
const href = (e: Evidence) => (e.kind === 'link' && typeof e.url === 'string' && /^https?:\/\//i.test(e.url) ? e.url : '')

async function send() {
  const ok = asking.value ? await inbox.requestChanges(reply.value) : await inbox.comment(reply.value)
  if (ok) { reply.value = ''; asking.value = false }
}
</script>

<template>
  <section class="win ib-detail" aria-labelledby="ibd-h">
    <div class="tb">
      <button v-if="phone" type="button" class="wbtn" aria-label="Back to the inbox" @click="$emit('close')"><Icon name="back" /></button>
      <h2 id="ibd-h" class="ell">{{ d?.album.title ?? 'Proposal' }}</h2>
      <span class="stripes"></span>
      <RouterLink v-if="albumId" class="wbtn txt" :to="`/library/${albumId}`"><LibTip title="Open album" :note="tip(inboxNotes.openAlbum)">Open album</LibTip></RouterLink>
      <button v-if="phone && d" type="button" class="wbtn q" aria-label="Note on this page" :aria-pressed="!!help.note" @click="toggleNote">?</button>
      <button v-if="!phone" type="button" class="wbtn" aria-label="Close the proposal" @click="$emit('close')"><Icon name="close" /></button>
    </div>
    <p v-if="inbox.detailLoading && !d" class="t2 pad" role="status">Loading the proposal...</p>
    <p v-if="inbox.detailError" class="bad-text pad" role="alert">{{ inbox.detailError }}</p>
    <div v-if="d" class="vscroll ib-body">
      <header class="ib-head pad">
        <Cover :url="cover" :size="phone ? 64 : 84" />
        <div class="col g4 grow">
          <h3 class="ib-title">{{ d.current.title }}</h3>
          <p class="t2 sm">{{ byLine }}<LibTip title="Revision" :note="tip(inboxNotes.revision)">revision {{ d.head }}</LibTip>{{ editedBy }}</p>
          <p class="t2 sm mono ell" :title="d.album.folder">{{ d.album.folder }}</p>
          <p v-if="!albumId" class="t2 sm">The album changed since; the recorded diff shows.</p>
          <div v-if="d.state !== 'pending' || d.changes_requested" class="line g8"><span class="pill" :class="stateClass(d.state, d.changes_requested)">{{ stateWord(d) }}</span></div>
          <p v-if="d.follow_up_of" class="t2 sm">Follows up <RouterLink :to="`/inbox/${d.follow_up_of}`">proposal {{ d.follow_up_of }}</RouterLink>.</p>
          <p v-if="d.group" class="t2 sm">Group {{ d.group }}. Each album in a group is reviewed and written on its own.</p>
        </div>
        <div v-if="d.current.confidence != null" class="lcd ib-conf" role="img" :aria-label="`Confidence ${d.current.confidence}%`">
          <span class="lcd-l">SURE</span><span class="lcd-big num"><LibTip title="Confidence" :note="tip(inboxNotes.confidence)">{{ d.current.confidence }}%</LibTip></span>
        </div>
      </header>

      <div class="note ib-why" role="note" aria-label="The agent's explanation">
        <div class="col g8 grow">
          <p class="ib-reason">{{ d.current.reason }}</p>
          <p v-if="d.current.uncertainties" class="t2"><span class="bold"><LibTip title="Not sure" :note="tip(inboxNotes.notSure)">Not sure:</LibTip></span> {{ d.current.uncertainties }}</p>
          <div v-if="d.current.evidence.length" class="ib-tapes" aria-label="Evidence">
            <template v-for="(e, i) in d.current.evidence" :key="i">
              <a v-if="href(e)" class="sug" :href="href(e)" target="_blank" rel="noopener noreferrer"><span class="sug-v"><LibTip title="Evidence" :note="i === 0 ? tip(inboxNotes.evidence) : ''">{{ tapeValue(e) }}</LibTip></span><span class="sug-s">{{ tapeSource(e) }} <span aria-hidden="true">↗</span></span></a>
              <span v-else class="sug tape"><span class="sug-v"><LibTip title="Evidence" :note="i === 0 ? tip(inboxNotes.evidence) : ''">{{ tapeValue(e) }}</LibTip></span><span class="sug-s">{{ tapeSource(e) }}</span></span>
            </template>
          </div>
        </div>
      </div>

      <section class="col" aria-label="Fields">
        <div class="ib-sec">
          <h3>Fields</h3>
          <span v-if="hasPending" class="line g8">
            <button type="button" class="link sm" @click="inbox.selectAll(true)">All on</button>
            <button type="button" class="link sm" @click="inbox.selectAll(false)">All off</button>
          </span>
        </div>
        <div ref="bank" class="ib-bank" :class="{ cabled: pairs.length, narrow: phone }">
          <LibTip v-if="pairs.length" title="Linked fields" :note="tip(cableNote)"><InboxCable :pairs="pairs" :bank="bank" :narrow="phone" /></LibTip>
          <InboxOp v-for="op in ops" :key="op.id" :op="op" :phone="phone" />
        </div>
        <p v-if="cascadeText" class="t2 sm pad ib-moved" role="status">{{ cascadeText }}</p>
        <p v-if="superseded.length" class="t2 sm pad">Earlier fields no longer in this revision: {{ superseded.map((o) => name(o.id)).join(', ') }}.</p>
      </section>

      <div v-if="writes" class="ib-lcds pad" role="group" aria-label="What Accept writes">
        <div class="lcd tight"><span class="lcd-big num">{{ writes.fields }}</span><span class="lcd-l">{{ writes.fields === 1 ? 'FIELD' : 'FIELDS' }}</span></div>
        <div class="lcd tight"><span class="lcd-big num">{{ writes.files }}</span><span class="lcd-l">{{ writes.files === 1 ? 'FILE' : 'FILES' }}</span></div>
        <div class="lcd tight"><span class="lcd-big num">{{ writes.tags }}</span><span class="lcd-l"><LibTip title="Tag writes" :note="tip(inboxNotes.tagWrites)">TAG WRITES</LibTip></span></div>
      </div>

      <p v-if="refusal" class="bad-text pad" role="alert">{{ refusal }}</p>
      <InboxDiff v-if="diff" :preview="diff.preview" :title="diff.title" :note="diff.note" :confirm="diff.confirm ? inbox.confirm : undefined" @toggle="inbox.toggleConfirm" />
      <p v-if="inbox.previewing" class="t2 sm pad" role="status">Checking the selected fields against the files...</p>

      <section v-if="d.acceptances.length" class="col" aria-label="Accepted">
        <div class="ib-sec"><h3>Accepted</h3></div>
        <div v-for="a in d.acceptances" :key="a.id" class="ib-acc">
          <div class="line g8 wrap">
            <span class="pill" :class="outcomeClass(a)">{{ outcomeWord(a) }}</span>
            <span class="sm">{{ a.ops.map(name).join(', ') }} · by {{ a.author }} {{ ago(a.created_at, now) }}</span>
          </div>
          <p class="t2 sm">
            <template v-if="a.outcome.files">{{ a.outcome.written ?? 0 }} of {{ count(a.outcome.files, 'file') }} written. </template>
            <template v-if="a.outcome.error">{{ a.outcome.error }} </template>
            <span v-if="a.outcome.expired">Change {{ a.change_id }} left the history.</span>
            <span v-else-if="phone">Change {{ a.change_id }} is in History on a desktop.</span>
            <RouterLink v-else :to="{ path: '/library', query: { history: '1' } }">Change {{ a.change_id }} in History</RouterLink>
          </p>
        </div>
      </section>

      <section class="col" aria-label="Talkback">
        <div class="ib-sec"><h3><LibTip title="Talkback" :note="tip(inboxNotes.talkback)">Talkback</LibTip></h3></div>
        <ol class="ib-thread">
          <li v-for="e in d.events" :key="e.id" class="ib-ev-row" :class="e.actor_kind">
            <span class="sm"><span class="bold">{{ eventLine(e, d.current.ops) }}</span> <span class="t2">{{ ago(e.at, now) }}</span></span>
            <p v-if="e.body" class="ib-body-text">{{ e.body }}</p>
          </li>
        </ol>
        <form class="col g8 pad" aria-label="Reply" @submit.prevent="send">
          <label class="lbl-f">Reply<textarea v-model="reply" class="ib-text" rows="3" maxlength="4000"></textarea></label>
          <div class="line g12 wrap">
            <span class="grow"></span>
            <button v-if="hasPending" type="button" class="rocker" role="switch" :aria-checked="asking" @click="asking = !asking">
              <span class="rk" aria-hidden="true">ON<span class="rk-case"><span class="rk-paddle"></span></span>OFF</span><span class="rk-t"><span class="rk-name"><LibTip title="Ask for changes" :note="tip(inboxNotes.ask)">Ask for changes</LibTip></span></span>
            </button>
            <button type="submit" class="key sm" :disabled="inbox.busy || !reply.trim()">{{ hasPending && asking ? 'Ask for changes' : 'Send comment' }}</button>
          </div>
        </form>
      </section>

      <div class="ib-msgs pad">
        <p v-if="inbox.error" class="bad-text" role="alert">{{ inbox.error }}</p>
        <p v-if="inbox.notice" class="t2" role="status">{{ inbox.notice }}</p>
      </div>

      <InboxBar v-if="!phone" />
    </div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 12px 14px; }
.rs .wrap { flex-wrap: wrap; }
.rs .ib-body { overflow-y: auto; }
.rs .ib-head { display: flex; align-items: flex-start; gap: 14px; }
.rs .ib-title { font-size: 19px; font-weight: 700; line-height: 1.25; }
.rs .ib-conf { flex: none; align-items: center; gap: 2px; padding: 8px 10px; }
.rs .ib-conf .lcd-big { font-size: 24px; }
.rs .ib-conf .lcd-l { font-size: 11px; }
.rs .ib-why { margin: 0 14px 12px; }
.rs .ib-reason, .rs .ib-body-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.rs .ib-tapes { display: flex; flex-wrap: wrap; gap: 6px 10px; }
.rs .sug.tape { cursor: default; }
.rs a.sug { text-decoration: none; }
.rs .ib-moved { border-bottom: 1px solid var(--line); }
.rs .ib-lcds { display: flex; flex-wrap: wrap; gap: 10px; }
.rs .ib-lcds .lcd { flex: 1 1 110px; flex-direction: row; align-items: baseline; gap: 8px; padding: 8px 12px; }
.rs .ib-lcds .lcd-big { font-size: 26px; }
.rs .ib-msgs:empty { display: none; }
.rs .ib-acc { display: flex; flex-direction: column; gap: 4px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.rs .ib-thread { display: flex; flex-direction: column; margin: 0; padding: 0; list-style: none; }
.rs .ib-ev-row { display: flex; flex-direction: column; gap: 4px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.rs .ib-ev-row.user { box-shadow: inset 3px 0 0 var(--lamp); }
.rs .ib-ev-row.system { color: var(--text-2); }
/* The cable runs in a gutter left of the rockers. */
.rs .ib-bank { position: relative; }
.rs .ib-bank.cabled { padding-left: 36px; }
.rs .ib-bank.cabled.narrow { padding-left: 26px; }
</style>
