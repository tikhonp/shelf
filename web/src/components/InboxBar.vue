<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { count } from '../lib/format'
import { opName, stateWord } from '../lib/inbox'
import { inboxNotes } from '../lib/notes'
import { held, retryable, selectable, useInbox, type DeclineSnapshot } from '../stores/inbox'
import InboxAcceptance from './InboxAcceptance.vue'
import LibTip from './LibTip.vue'

defineProps<{ phone?: boolean }>()
const emit = defineEmits<{ files: [acceptanceId?: number] }>()
const inbox = useInbox()
const d = computed(() => inbox.detail)
const declineTrigger = ref<HTMLButtonElement>()
const declineInput = ref<HTMLInputElement>()
const canReview = computed(() => !!d.value && d.value.ops.some((op) => selectable(d.value!, op.id) && !held(d.value!, op.id)))
const behind = computed(() => !!d.value && inbox.dirty && inbox.headSeen !== 0 && inbox.headSeen !== d.value.head)
const hasConflicts = computed(() => inbox.draftConflicts.length > 0)
const accepted = computed(() => d.value?.acceptances.at(-1))
const justAccepted = computed(() => d.value?.acceptances.find((a) => a.id === inbox.lastAccepted?.acceptance.id))
const checking = computed(() => inbox.previewing || inbox.detailRefreshing)
const canRecheck = computed(() => inbox.shown.length > 0 && !checking.value && !inbox.preview && !inbox.busy)
const retries = computed(() => d.value?.ops.filter((o) => retryable(d.value!, o)) ?? [])
const retrySelected = computed(() => retries.value.some((o) => inbox.shown.includes(o.id)))
const mode = computed(() => !d.value ? '' : inbox.dirty ? 'dirty' : canReview.value || inbox.declineCount ? 'review' : 'result')
const declineChanged = computed(() => {
  const snap = inbox.declineSnap
  if (!inbox.declining || !snap || !d.value) return false
  if (d.value.head !== snap.revision) return true
  const now = inbox.declineTargets
  return now.length !== snap.ops.length || now.some((id, i) => id !== snap.ops[i])
})
const declineRevisionChanged = computed(() => !!inbox.declineSnap && !!d.value && d.value.head !== inbox.declineSnap.revision)
const hasStatus = computed(
  () =>
    (mode.value === 'review' && (retries.value.length || refusal.value || inbox.warnings.length || inbox.reviewRequired || canRecheck.value)) ||
    inbox.draftConflicts.length > 0 ||
    inbox.droppedDrafts.length > 0 ||
    !!inbox.error ||
    !!inbox.notice,
)
const refusal = computed(() => {
  const r = inbox.shown.length ? inbox.currentSelection?.refusal : d.value?.live?.refusal
  if (!r || !d.value) return ''
  const match = /^operation (\S+) requires (\S+)$/.exec(r.error)
  if (!match) return r.error
  const name = (id: string) => opName(d.value!.current.ops, id)
  return `${name(match[1]!)} goes with ${name(match[2]!)}, which can no longer be accepted with it.`
})
const summary = computed(() => {
  if (checking.value) return 'Checking the files...'
  const w = inbox.writes
  if (w) return [count(w.fields, 'field'), count(w.files, 'file'), count(w.tags, 'tag write'), w.folder && count(w.folder, 'folder artwork change')].filter(Boolean).join(' · ')
  if (!canReview.value && inbox.declineCount) return 'The files already hold every pending field.'
  return inbox.shown.length ? 'Preview unavailable' : 'Switch on the fields to accept.'
})

function snapshotDecline(): DeclineSnapshot | null {
  if (!d.value || inbox.declineTargets.length === 0) return null
  const ops = [...inbox.declineTargets]
  return {
    revision: d.value.head,
    ops,
    names: ops.map((id) => opName(d.value!.current.ops, id)),
  }
}
function openDecline() {
  const snap = snapshotDecline()
  if (!snap) return
  inbox.declineSnap = snap
  inbox.declining = true
  void nextTick(() => declineInput.value?.focus())
}
function refreshDecline() {
  inbox.declineSnap = snapshotDecline()
}
watch(() => d.value?.id, (next, prev) => {
  if (prev !== undefined && next !== prev) cancelDecline()
})
function cancelDecline(restoreFocus = false) {
  inbox.declineReason = ''
  inbox.declining = false
  inbox.declineSnap = null
  if (restoreFocus) void nextTick(() => declineTrigger.value?.focus())
}
async function declineRest() {
  const snap = inbox.declineSnap
  if (!snap || declineChanged.value || inbox.busy || checking.value) return
  if (await inbox.declineRest(inbox.declineReason, { revision: snap.revision, ops: snap.ops })) cancelDecline()
}
async function accept() {
  if (await inbox.accept()) emit('files', inbox.lastAccepted?.acceptance.id)
}
const droppedReason = (reasonWord: string) => (reasonWord === 'withdrawn' ? 'the proposal was withdrawn' : `the field was ${reasonWord}`)
</script>

<template>
  <component :is="phone ? 'section' : 'div'" v-if="mode" :class="phone ? 'win slot ib-slot' : 'ib-bar'" role="region" :aria-label="mode === 'dirty' ? 'Unsaved values' : mode === 'result' ? 'Result' : 'Review'">
    <div v-if="hasStatus" class="ib-status col g8">
      <template v-if="mode === 'review'">
        <p v-if="retries.length" class="t2 sm"><strong>{{ count(retries.length, 'field') }} can be retried.</strong> The original write failed before it wrote any files. See Talkback for the error.</p>
        <p v-if="refusal" class="bad-text sm" role="alert">{{ refusal }}</p>
        <div v-if="inbox.reviewRequired && inbox.preview" class="line g8 wrap sm" role="status">
          <span>Review the updated changes before accepting.</span>
          <button type="button" class="link sm" @click="emit('files')">Review Files</button>
        </div>
        <button v-if="canRecheck" type="button" class="link sm ib-recheck" @click="inbox.checkFiles()">Check files again</button>
        <div v-for="w in inbox.warnings" :key="w.kind" class="note ib-warn">
          <button type="button" class="rocker flat" role="switch" :aria-checked="inbox.confirm.includes(w.kind)" @click="inbox.toggleConfirm(w.kind)">
            <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span>
            <span class="ib-warn-t">{{ w.message }}</span>
          </button>
        </div>
      </template>
      <div v-for="c in inbox.draftConflicts" :key="`conflict-${c.opId}`" class="note ib-conflict col g4 sm" role="alert">
        <span>Your draft for <strong>{{ c.originalLabel }}</strong> conflicts with revision {{ d!.head }} ({{ c.reason }}).</span>
        <span v-if="c.text" class="t2">Draft: <span class="mono">{{ c.text }}</span></span>
        <button type="button" class="link sm ib-recheck" @click="inbox.discard(c.opId)">Discard {{ c.originalLabel }} draft</button>
      </div>
      <div v-for="item in inbox.droppedDrafts" :key="`dropped-${item.opId}`" class="note ib-dropped col g4 sm" role="status">
        <span>Your unsaved edit to <strong>{{ item.label }}</strong> was dropped because {{ droppedReason(item.reason) }}.</span>
        <span v-if="item.text" class="t2">Draft: <span class="mono">{{ item.text }}</span></span>
        <button type="button" class="link sm ib-recheck" @click="inbox.dismissDroppedDraft(item.opId)">Dismiss</button>
      </div>
      <p v-if="inbox.error" class="bad-text sm" role="alert">{{ inbox.error }}</p>
      <p v-if="inbox.notice" class="t2 sm" role="status">{{ inbox.notice }}</p>
    </div>
    <div class="lib-save ib-bar-in">
      <template v-if="mode === 'dirty'">
        <div class="col g4 grow">
          <span class="bold">Your values aren't saved</span>
          <span v-if="!phone" class="sub">Save values makes a new revision. You can accept it after saving.</span>
          <span v-if="hasConflicts" class="sub">Resolve conflicting drafts before saving on revision {{ d!.head }}.</span>
          <span v-else-if="behind" class="sub">Revision {{ d!.head }} arrived after you started. <button type="button" class="link" @click="inbox.rebase()">Keep my values on revision {{ d!.head }}</button></span>
        </div>
        <div class="line g8">
          <button type="button" class="key" :class="{ sm: !phone }" :disabled="inbox.busy" @click="inbox.discard()">Discard</button>
          <button type="button" class="key sun" :class="{ sm: !phone }" :disabled="inbox.busy || behind || hasConflicts" @click="inbox.saveValues()">Save values</button>
        </div>
      </template>
      <form v-else-if="inbox.declining" class="ib-decline-form col g8 grow" @submit.prevent="declineRest">
        <p v-if="inbox.declineSnap" class="t2 sm">Declining: <strong>{{ (inbox.declineSnap.names ?? inbox.declineSnap.ops).join(', ') }}</strong> (revision {{ inbox.declineSnap.revision }})</p>
        <div v-if="declineChanged" class="line g8 wrap sm bad-text" role="alert">
          <span>{{ declineRevisionChanged ? `Revision ${d!.head} arrived while Decline was open.` : 'The fields to decline changed while this form was open.' }}</span>
          <button v-if="inbox.declineTargets.length" type="button" class="link sm" @click="refreshDecline">Update to current {{ count(inbox.declineTargets.length, 'field') }}</button>
          <span v-else>No pending fields are left to decline.</span>
        </div>
        <div class="line g8 wrap ib-decline-row">
          <label class="lbl-f grow">Why decline {{ inbox.declineSnap && inbox.declineSnap.ops.length === d!.summary.pending ? 'every pending field' : 'the rest' }}? Optional<input ref="declineInput" v-model="inbox.declineReason" class="lib-in" maxlength="4000" /></label>
          <div class="line g8">
            <button type="button" class="key" :class="{ sm: !phone }" @click="cancelDecline(true)">Cancel</button>
            <button type="submit" class="key sun" :class="{ sm: !phone }" :disabled="inbox.busy || checking || declineChanged || !inbox.declineSnap || inbox.declineTargets.length === 0">Decline {{ count(inbox.declineSnap?.ops.length ?? 0, 'field') }}</button>
          </div>
        </div>
      </form>
      <template v-else-if="mode === 'review'">
        <div v-if="justAccepted && !retries.length" class="ib-out">
          <InboxAcceptance :acceptance="justAccepted" :album-id="d?.album.current_id ?? 0" :phone="phone" />
        </div>
        <div :class="phone ? 'ib-sum' : 'lcd'" role="status">
          <span :class="phone ? 'sm' : 'lcd-l'">{{ summary }}<template v-if="inbox.shown.length && !inbox.confirmed">. Confirm each warning first.</template></span>
        </div>
        <div class="line g8 wrap ib-review-actions">
          <button v-if="inbox.declineCount" ref="declineTrigger" type="button" class="key" :class="{ sm: !phone }" :disabled="inbox.busy || checking" @click="openDecline"><template v-if="phone">Decline {{ inbox.declineCount }}</template><LibTip v-else :title="inbox.shown.length ? 'Decline the rest' : 'Decline all'" :note="inbox.shown.length ? inboxNotes.declineRest : inboxNotes.declineAll">{{ inbox.shown.length ? 'Decline the rest' : 'Decline all' }}</LibTip></button>
          <button type="button" class="key sun" :class="{ sm: !phone }" :disabled="!inbox.canAccept" @click="accept"><template v-if="phone">{{ retrySelected ? 'Retry' : 'Accept' }} {{ inbox.shown.length }}</template><LibTip v-else title="Accept" :note="inboxNotes.accept">{{ retrySelected ? 'Accept again' : 'Accept' }} {{ count(inbox.shown.length, 'field') }}</LibTip></button>
        </div>
      </template>
      <div v-else class="ib-out col g4">
        <strong class="sm">{{ stateWord(d!) }}</strong>
        <InboxAcceptance v-if="accepted" :acceptance="accepted" :album-id="d?.album.current_id ?? 0" :phone="phone" />
      </div>
    </div>
  </component>
</template>

<style scoped>
.rs .ib-bar, .rs .ib-slot { flex: none; display: flex; flex-direction: column; max-height: 55%; overflow: hidden; background: var(--win-2); }
.rs .win.slot.ib-slot { animation: none; }
.rs .ib-bar { border-top: 1px solid var(--ink); }
.rs .ib-bar-in { flex-wrap: wrap; }
.rs .ib-out { flex-basis: 100%; }
.rs .ib-status { min-height: 0; padding: 8px 14px; overflow-y: auto; overflow-wrap: anywhere; }
.rs .ib-status > * { flex: none; }
.rs .ib-recheck { align-self: flex-start; }
.rs .ib-bar .ib-status + .lib-save { border-top: 0; }
.rs .ib-warn { padding: 4px 10px; border-color: var(--yellow); }
.rs .ib-conflict { padding: 8px 10px; border-color: var(--red, var(--bad)); }
.rs .ib-dropped { padding: 8px 10px; border-color: var(--yellow); }
.rs .ib-decline-form { width: 100%; }
.rs .ib-decline-row { align-items: flex-end; }
.rs .ib-warn .rocker { min-height: 40px; text-align: left; }
.rs .ib-warn-t { font-size: 14px; white-space: normal; }
.rs .ib-bar .lbl-f { flex: 1 1 220px; }
.rs .ib-slot .ib-bar-in { padding: 0 8px 8px; }
.rs .ib-slot .ib-bar-in:first-child { padding-top: 8px; }
.rs .ib-slot .ib-status { padding: 8px; }
.rs .ib-slot .ib-sum { flex: 1 1 100px; min-width: 0; color: var(--text-2); }
.rs .ib-slot .lbl-f { flex-basis: 100%; }
</style>
