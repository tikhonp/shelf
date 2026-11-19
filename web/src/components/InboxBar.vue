<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { count } from '../lib/format'
import { opName, stateWord } from '../lib/inbox'
import { inboxNotes } from '../lib/notes'
import { held, retryable, selectable, useInbox } from '../stores/inbox'
import InboxAcceptance from './InboxAcceptance.vue'
import LibTip from './LibTip.vue'

defineProps<{ phone?: boolean }>()
const emit = defineEmits<{ files: [acceptanceId?: number] }>()
const inbox = useInbox()
const d = computed(() => inbox.detail)
const reason = ref('')
const declining = ref(false)
const canReview = computed(() => !!d.value && d.value.ops.some((op) => selectable(d.value!, op.id) && !held(d.value!, op.id)))
const behind = computed(() => !!d.value && inbox.dirty && inbox.headSeen !== 0 && inbox.headSeen !== d.value.head)
const accepted = computed(() => d.value?.acceptances.at(-1))
const justAccepted = computed(() => d.value?.acceptances.find((a) => a.id === inbox.lastAccepted?.acceptance.id))
const checking = computed(() => inbox.previewing || inbox.detailRefreshing)
const canRecheck = computed(() => inbox.shown.length > 0 && !checking.value && !inbox.preview && !inbox.busy)
const retries = computed(() => d.value?.ops.filter((o) => retryable(d.value!, o)) ?? [])
const retrySelected = computed(() => retries.value.some((o) => inbox.shown.includes(o.id)))
const mode = computed(() => !d.value ? '' : inbox.dirty ? 'dirty' : canReview.value || inbox.declineCount ? 'review' : 'result')
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
  return inbox.shown.length ? 'Preview unavailable' : 'Switch on the fields to accept.'
})

watch([mode, () => inbox.declineCount], () => {
  if (mode.value !== 'review' || !inbox.declineCount) declining.value = false
})
function cancelDecline() {
  reason.value = ''
  declining.value = false
}
async function declineRest() {
  if (await inbox.declineRest(reason.value)) cancelDecline()
}
async function accept() {
  if (await inbox.accept()) emit('files', inbox.lastAccepted?.acceptance.id)
}
</script>

<template>
  <component :is="phone ? 'section' : 'div'" v-if="mode" :class="phone ? 'win slot ib-slot' : 'ib-bar'" role="region" :aria-label="mode === 'dirty' ? 'Unsaved values' : mode === 'result' ? 'Result' : 'Review'">
    <div v-if="(mode === 'review' && (retries.length || refusal || inbox.warnings.length || inbox.reviewRequired || canRecheck)) || inbox.error || inbox.notice" class="ib-status col g8">
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
      <p v-if="inbox.error" class="bad-text sm" role="alert">{{ inbox.error }}</p>
      <p v-if="inbox.notice" class="t2 sm" role="status">{{ inbox.notice }}</p>
    </div>
    <div class="lib-save ib-bar-in">
      <template v-if="mode === 'dirty'">
        <div class="col g4 grow">
          <span class="bold">Your values aren't saved</span>
          <span v-if="!phone" class="sub">Save values makes a new revision. You can accept it after saving.</span>
          <span v-if="behind" class="sub">Revision {{ d!.head }} arrived after you started. <button type="button" class="link" @click="inbox.rebase()">Keep my values on revision {{ d!.head }}</button></span>
        </div>
        <div class="line g8">
          <button type="button" class="key" :class="{ sm: !phone }" :disabled="inbox.busy" @click="inbox.discard()">Discard</button>
          <button type="button" class="key sun" :class="{ sm: !phone }" :disabled="inbox.busy || behind" @click="inbox.saveValues()">Save values</button>
        </div>
      </template>
      <template v-else-if="mode === 'review' && declining">
        <label class="lbl-f grow">Why decline {{ inbox.shown.length ? 'the rest' : 'every pending field' }}? Optional<input v-model="reason" class="lib-in" maxlength="4000" /></label>
        <div class="line g8">
          <button type="button" class="key" :class="{ sm: !phone }" @click="cancelDecline">Cancel</button>
          <button type="button" class="key sun" :class="{ sm: !phone }" :disabled="inbox.busy || checking" @click="declineRest">Decline {{ count(inbox.declineCount, 'field') }}</button>
        </div>
      </template>
      <template v-else-if="mode === 'review'">
        <div v-if="justAccepted && !retries.length" class="ib-out">
          <InboxAcceptance :acceptance="justAccepted" :album-id="d?.album.current_id ?? 0" :phone="phone" />
        </div>
        <div :class="phone ? 'ib-sum' : 'lcd'" role="status">
          <span :class="phone ? 'sm' : 'lcd-l'">{{ summary }}<template v-if="inbox.shown.length && !inbox.confirmed">. Confirm each warning first.</template></span>
        </div>
        <div class="line g8 wrap ib-review-actions">
          <button v-if="inbox.declineCount" type="button" class="key" :class="{ sm: !phone }" :disabled="inbox.busy || checking" @click="declining = true"><template v-if="phone">Decline {{ inbox.declineCount }}</template><LibTip v-else :title="inbox.shown.length ? 'Decline the rest' : 'Decline all'" :note="inbox.shown.length ? inboxNotes.declineRest : inboxNotes.declineAll">{{ inbox.shown.length ? 'Decline the rest' : 'Decline all' }}</LibTip></button>
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
.rs .ib-warn .rocker { min-height: 40px; text-align: left; }
.rs .ib-warn-t { font-size: 14px; white-space: normal; }
.rs .ib-bar .lbl-f { flex: 1 1 220px; }
.rs .ib-slot .ib-bar-in { padding: 0 8px 8px; }
.rs .ib-slot .ib-bar-in:first-child { padding-top: 8px; }
.rs .ib-slot .ib-status { padding: 8px; }
.rs .ib-slot .ib-sum { flex: 1 1 100px; min-width: 0; color: var(--text-2); }
.rs .ib-slot .lbl-f { flex-basis: 100%; }
</style>
