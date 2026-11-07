<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { Evidence } from '../api/agents'
import { useNow } from '../lib/clock'
import { ago } from '../lib/format'
import { eventLine, outcomeClass, outcomeWord, stateClass, stateWord } from '../lib/inbox'
import { selectable, useInbox } from '../stores/inbox'
import Icon from './Icon.vue'
import InboxDiff from './InboxDiff.vue'
import InboxOp from './InboxOp.vue'

defineProps<{ phone?: boolean }>()
defineEmits<{ close: [] }>()
const inbox = useInbox()
const now = useNow()

const d = computed(() => inbox.detail)
const reason = ref('')
const changes = ref('')
const remark = ref('')

const albumId = computed(() => d.value?.album.current_id ?? 0)
const head = computed(() => d.value?.revisions.find((r) => r.revision === d.value?.head))
const hasPending = computed(() => (d.value?.summary.pending ?? 0) > 0 && d.value?.state !== 'withdrawn')
const canReview = computed(() => !!d.value && d.value.ops.some((op) => selectable(d.value!, op.id)))
const superseded = computed(() => d.value?.ops.filter((o) => !d.value!.current.ops.some((c) => c.id === o.id)) ?? [])
const behind = computed(() => !!d.value && inbox.dirty && inbox.headSeen !== 0 && inbox.headSeen !== d.value.head)

// The diff the reviewer accepts is the one for the selection; with nothing selected the page shows every pending field.
const diff = computed(() => {
  const v = d.value
  if (!v) return null
  const s = inbox.selection
  if (inbox.selected.length > 0 && s?.preview) return { preview: s.preview, title: `Selected · ${inbox.selected.length} ${inbox.selected.length === 1 ? 'field' : 'fields'}`, note: 'What Accept writes', confirm: true }
  if (v.live?.preview && hasPending.value) return { preview: v.live.preview, title: 'All pending fields', note: 'Against the files now', confirm: false }
  return { preview: v.current.preview, title: 'As proposed', note: 'Recorded with the revision', confirm: false }
})
const refusal = computed(() => (inbox.selected.length > 0 ? inbox.selection?.refusal : d.value?.live?.refusal) ?? null)

const evidenceText = (e: Evidence) =>
  e.text ?? ([e.kind, e.source, e.field, e.key, e.anchor].filter((x) => typeof x === 'string' && x).join(' · ') || e.kind)

async function decline(remaining: boolean) {
  if (await inbox.decline(remaining, reason.value)) reason.value = ''
}
async function request() {
  if (await inbox.requestChanges(changes.value)) changes.value = ''
}
async function comment() {
  if (await inbox.comment(remark.value)) remark.value = ''
}
</script>

<template>
  <section class="win ib-detail" aria-labelledby="ibd-h">
    <div class="tb">
      <button v-if="phone" type="button" class="wbtn" aria-label="Back to the inbox" @click="$emit('close')"><Icon name="back" /></button>
      <h2 id="ibd-h">Proposal {{ inbox.detailId }}</h2>
      <span class="stripes"></span>
      <button v-if="!phone" type="button" class="wbtn" aria-label="Close the proposal" @click="$emit('close')"><Icon name="close" /></button>
    </div>
    <p v-if="inbox.detailLoading && !d" class="t2 pad" role="status">Loading the proposal...</p>
    <p v-if="inbox.detailError" class="bad-text pad" role="alert">{{ inbox.detailError }}</p>
    <div v-if="d" class="vscroll ib-body">
      <header class="col g4 pad">
        <h3 class="ib-title">{{ d.current.title }}</h3>
        <p class="sub">
          <RouterLink v-if="albumId" :to="`/library/${albumId}`">{{ d.album.title }}</RouterLink><span v-else>{{ d.album.title }}</span>
          · {{ d.album.artist }}
        </p>
        <p class="t2 sm">{{ d.album.folder }}<template v-if="!albumId"> · The album changed since; the recorded diff shows.</template></p>
        <div class="line g8 wrap">
          <span class="pill" :class="stateClass(d.state, d.changes_requested)">{{ stateWord(d) }}</span>
          <span class="t2 sm">By {{ d.connection.name }} · revision {{ d.head }}<template v-if="head"> by {{ head.author }} {{ ago(head.created_at, now) }}</template></span>
        </div>
        <p v-if="d.follow_up_of" class="t2 sm">Follows up <RouterLink :to="`/inbox/${d.follow_up_of}`">proposal {{ d.follow_up_of }}</RouterLink>.</p>
        <p v-if="d.group" class="t2 sm">Group {{ d.group }}. Each album in a group is reviewed and written on its own.</p>
      </header>

      <section class="col g8 pad ib-why" aria-label="The agent's explanation">
        <p class="ib-reason">{{ d.current.reason }}</p>
        <p v-if="d.current.uncertainties" class="t2"><span class="bold">Uncertain:</span> {{ d.current.uncertainties }}</p>
        <p v-if="d.current.confidence != null" class="t2 sm">The agent rates its confidence {{ d.current.confidence }}%. That explains the proposal and never applies it.</p>
        <ul v-if="d.current.evidence.length" class="ib-ev">
          <li v-for="(e, i) in d.current.evidence" :key="i" class="sm"><span class="mono t2">{{ e.kind }}</span> {{ evidenceText(e) }}</li>
        </ul>
      </section>

      <section class="col" aria-label="Fields">
        <div class="ib-sec">
          <h3>Fields</h3>
          <span v-if="hasPending" class="line g8">
            <button type="button" class="link sm" @click="inbox.selectAll(true)">Select all pending</button>
            <button type="button" class="link sm" @click="inbox.selectAll(false)">Select none</button>
          </span>
        </div>
        <InboxOp v-for="op in d.current.ops" :key="op.id" :op="op" />
        <p v-if="superseded.length" class="t2 sm pad">Earlier fields no longer in this revision: {{ superseded.map((o) => o.id).join(', ') }}.</p>
      </section>

      <div v-if="inbox.dirty" class="note ib-save" role="region" aria-label="Unsaved values">
        <div class="col g4 grow">
          <span class="bold">Your values aren't saved</span>
          <span class="sub">Saving makes a new revision with your name and writes nothing to the library. Accept waits until then.</span>
          <span v-if="behind" class="sub">Revision {{ d.head }} arrived after you started. <button type="button" class="link" @click="inbox.rebase()">Keep my values on revision {{ d.head }}</button></span>
        </div>
        <button type="button" class="key sm" :disabled="inbox.busy" @click="inbox.discard()">Discard</button>
        <button type="button" class="key sm sun" :disabled="inbox.busy || behind" @click="inbox.saveValues()">Save values</button>
      </div>

      <p v-if="refusal" class="bad-text pad" role="alert">{{ refusal.error }}</p>
      <InboxDiff v-if="diff" :preview="diff.preview" :title="diff.title" :note="diff.note" :confirm="diff.confirm ? inbox.confirm : undefined" @toggle="inbox.toggleConfirm" />
      <p v-if="inbox.previewing" class="t2 sm pad" role="status">Checking the selected fields against the files...</p>

      <div class="ib-msgs pad">
        <p v-if="inbox.error" class="bad-text" role="alert">{{ inbox.error }}</p>
        <p v-if="inbox.notice" class="t2" role="status">{{ inbox.notice }}</p>
      </div>

      <section v-if="canReview" class="col g10 pad ib-actions" aria-label="Review">
        <div class="line g10 wrap">
          <button type="button" class="key sun" :disabled="!inbox.canAccept" @click="inbox.accept()">Accept selected</button>
          <span v-if="inbox.selected.length && !inbox.confirmed" class="t2 sm">Confirm each warning first.</span>
        </div>
        <label v-if="hasPending" class="lbl-f">Reason for declining, optional<input v-model="reason" class="lib-in" maxlength="4000" /></label>
        <div v-if="hasPending" class="line g10 wrap">
          <button type="button" class="key" :disabled="inbox.busy || inbox.pendingSelected.length === 0" @click="decline(false)">Decline selected</button>
          <button type="button" class="key" :disabled="inbox.busy" @click="decline(true)">Decline remaining</button>
        </div>
        <label v-if="hasPending" class="lbl-f">Changes you want<textarea v-model="changes" class="ib-text" rows="3" maxlength="4000"></textarea></label>
        <button v-if="hasPending" type="button" class="key self" :disabled="inbox.busy || !changes.trim()" @click="request">Request changes</button>
      </section>

      <section v-if="d.acceptances.length" class="col" aria-label="Accepted">
        <div class="ib-sec"><h3>Accepted</h3></div>
        <div v-for="a in d.acceptances" :key="a.id" class="ib-acc">
          <div class="line g8 wrap">
            <span class="pill" :class="outcomeClass(a)">{{ outcomeWord(a) }}</span>
            <span class="sm">{{ a.ops.join(', ') }} · revision {{ a.revision }} · by {{ a.author }} {{ ago(a.created_at, now) }}</span>
          </div>
          <p class="t2 sm">
            <template v-if="a.outcome.files">{{ a.outcome.written ?? 0 }} of {{ a.outcome.files }} files written. </template>
            <template v-if="a.outcome.error">{{ a.outcome.error }} </template>
            <RouterLink v-if="!a.outcome.expired" :to="{ path: '/library', query: { history: '1' } }">Library change {{ a.change_id }} in History</RouterLink>
            <span v-else>Library change {{ a.change_id }} left the history.</span>
          </p>
        </div>
      </section>

      <section class="col" aria-label="Discussion">
        <div class="ib-sec"><h3>Discussion</h3></div>
        <ol class="ib-thread">
          <li v-for="e in d.events" :key="e.id" class="ib-ev-row" :class="e.actor_kind">
            <span class="sm"><span class="bold">{{ eventLine(e) }}</span> <span class="t2">{{ ago(e.at, now) }}</span></span>
            <p v-if="e.body" class="ib-body-text">{{ e.body }}</p>
          </li>
        </ol>
        <div class="col g8 pad">
          <label class="lbl-f">Comment<textarea v-model="remark" class="ib-text" rows="3" maxlength="4000"></textarea></label>
          <p class="t2 sm">A comment decides nothing. The agent reads it on its next run.</p>
          <button type="button" class="key self" :disabled="inbox.busy || !remark.trim()" @click="comment">Comment</button>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 12px 14px; }
.rs .wrap { flex-wrap: wrap; }
.rs .self { align-self: flex-start; }
.rs .ib-body { overflow-y: auto; }
.rs .ib-title { font-size: 19px; font-weight: 700; line-height: 1.25; }
.rs .ib-why { border-top: 1px solid var(--ink); border-bottom: 1px solid var(--ink); background: var(--win-2); }
.rs .ib-reason, .rs .ib-body-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.rs .ib-ev { display: flex; flex-direction: column; gap: 4px; margin: 0; padding-left: 18px; }
.rs .ib-save { margin: 12px 14px; align-items: center; flex-wrap: wrap; border-color: var(--sun); }
.rs .ib-msgs:empty { display: none; }
.rs .ib-actions { border-top: 1px solid var(--ink); }
.rs .ib-acc { display: flex; flex-direction: column; gap: 4px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.rs .ib-thread { display: flex; flex-direction: column; margin: 0; padding: 0; list-style: none; }
.rs .ib-ev-row { display: flex; flex-direction: column; gap: 4px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.rs .ib-ev-row.user { box-shadow: inset 3px 0 0 var(--lamp); }
.rs .ib-ev-row.system { color: var(--text-2); }
</style>
