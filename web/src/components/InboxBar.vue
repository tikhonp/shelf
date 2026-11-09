<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { count } from '../lib/format'
import { outcomeClass, outcomeWord } from '../lib/inbox'
import { selectable, useInbox } from '../stores/inbox'

// The review bar. On the desktop it sits at the foot of the proposal's window, where the Library's Save bar does.
// On the phone it is a slot above the dock, so Accept is never a long scroll away.
const props = defineProps<{ phone?: boolean }>()
const inbox = useInbox()
const d = computed(() => inbox.detail)
const reason = ref('')
const declining = ref(false)

const canReview = computed(() => !!d.value && d.value.ops.some((op) => selectable(d.value!, op.id)))
const behind = computed(() => !!d.value && inbox.dirty && inbox.headSeen !== 0 && inbox.headSeen !== d.value.head)
// accepted is the acceptance this reviewer just made, once the detail lists it with its outcome.
const accepted = computed(() => {
  const a = inbox.lastAccepted
  return (a && d.value?.acceptances.find((x) => x.id === a.acceptance.id)) || null
})
const mode = computed(() => (!d.value ? '' : inbox.dirty ? 'dirty' : canReview.value || accepted.value ? 'review' : ''))

async function declineRest() {
  if (await inbox.declineRest(reason.value)) { reason.value = ''; declining.value = false }
}
const summary = computed(() => {
  const w = inbox.writes
  if (w) return props.phone ? `${count(w.fields, 'field')} · ${count(w.files, 'file')}` : `Accept writes ${count(w.fields, 'field')} to ${count(w.files, 'file')}. History can undo it.`
  return inbox.selected.length === 0 ? 'Switch on the fields to accept.' : ''
})
</script>

<template>
  <component :is="phone ? 'section' : 'div'" v-if="mode" :class="phone ? 'win slot ib-slot' : 'ib-bar'" role="region" :aria-label="mode === 'dirty' ? 'Unsaved values' : 'Review'">
    <div class="ib-bar-in" :class="{ 'lib-save': phone }">
      <template v-if="mode === 'dirty'">
        <div class="col g4 grow">
          <span class="bold">Your values aren't saved</span>
          <span v-if="!phone" class="sub">Saving makes a new revision with your name and writes nothing to the library. Accept waits until then.</span>
          <span v-if="behind" class="sub">Revision {{ d!.head }} arrived after you started. <button type="button" class="link" @click="inbox.rebase()">Keep my values on revision {{ d!.head }}</button></span>
        </div>
        <div class="line g8">
          <button type="button" class="key" :class="{ sm: !phone }" :disabled="inbox.busy" @click="inbox.discard()">Discard</button>
          <button type="button" class="key sun" :class="{ sm: !phone }" :disabled="inbox.busy || behind" @click="inbox.saveValues()">Save values</button>
        </div>
      </template>
      <template v-else>
        <div v-if="accepted" class="line g8 wrap ib-out">
          <span class="pill" :class="outcomeClass(accepted)">{{ outcomeWord(accepted) }}</span>
          <span class="sm"><template v-if="accepted.outcome.files">{{ accepted.outcome.written ?? 0 }} of {{ count(accepted.outcome.files, 'file') }}</template></span>
          <RouterLink v-if="!accepted.outcome.expired" class="sm" :to="{ path: '/library', query: { history: '1' } }">Change {{ accepted.change_id }} in History</RouterLink>
        </div>
        <template v-if="canReview && declining">
          <label class="lbl-f grow">Why decline {{ inbox.selected.length ? 'the rest' : 'every field' }}? Optional<input v-model="reason" class="lib-in" maxlength="4000" /></label>
          <div class="line g8">
            <button type="button" class="key" :class="{ sm: !phone }" @click="declining = false">Cancel</button>
            <button type="button" class="key sun" :class="{ sm: !phone }" :disabled="inbox.busy" @click="declineRest">Decline {{ count(inbox.rest.length, 'field') }}</button>
          </div>
        </template>
        <template v-else-if="canReview">
          <span class="sm grow ib-sum">{{ summary }}<template v-if="inbox.selected.length && !inbox.confirmed"> Confirm each warning first.</template></span>
          <div class="line g8">
            <button v-if="inbox.rest.length" type="button" class="key" :class="{ sm: !phone }" :disabled="inbox.busy" @click="declining = true">{{ phone ? 'Decline' : inbox.selected.length ? 'Decline the rest' : 'Decline all' }}</button>
            <button type="button" class="key sun" :class="{ sm: !phone }" :disabled="!inbox.canAccept" @click="inbox.accept()">Accept {{ phone ? inbox.selected.length : count(inbox.selected.length, 'field') }}</button>
          </div>
        </template>
      </template>
    </div>
  </component>
</template>

<style scoped>
.rs .ib-bar { position: sticky; bottom: 0; z-index: 2; display: flex; flex-wrap: wrap; align-items: center; gap: 10px 14px; margin-top: auto; padding: 12px 14px; background: var(--win-2); border-top: 1px solid var(--ink); box-shadow: 0 -4px 10px rgba(0, 0, 0, .25); }
.rs .ib-bar .ib-bar-in { display: contents; }
.rs .ib-bar .lbl-f { min-width: 200px; }
.rs .ib-out { flex-basis: 100%; }
.rs .wrap { flex-wrap: wrap; }
.rs .ib-slot .ib-bar-in { flex-wrap: wrap; }
.rs .ib-slot .ib-sum { flex: 1 1 0; min-width: 0; color: var(--text-2); }
.rs .ib-slot .lbl-f { flex-basis: 100%; }
</style>
