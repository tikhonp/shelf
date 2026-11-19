<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Op } from '../api/agents'
import { opName } from '../lib/inbox'
import { inboxNotes } from '../lib/notes'
import { retryable, selectable, useInbox } from '../stores/inbox'
import Icon from './Icon.vue'
import InboxCable from './InboxCable.vue'
import InboxOp from './InboxOp.vue'
import LibTip from './LibTip.vue'

const props = defineProps<{ id: string; label: string; ops: Op[]; phone?: boolean; folded: boolean }>()
const emit = defineEmits<{ fold: []; reveal: [id: string] }>()
const inbox = useInbox()
const bank = ref<HTMLElement>()
const eligible = computed(() => props.ops.filter((o) => inbox.detail && selectable(inbox.detail, o.id)).map((o) => o.id))
const on = computed(() => eligible.value.filter((id) => inbox.selected.includes(id)).length)
const decisions = computed(() => inbox.detail?.ops.filter((decision) => props.ops.some((o) => o.id === decision.id)) ?? [])
const summary = computed(() => [
  eligible.value.length ? `${on.value} of ${eligible.value.length} on` : '',
  decisions.value.some((o) => retryable(inbox.detail!, o)) ? 'Retry available' : '',
  ...(['accepted', 'declined'] as const).map((state) => {
    const n = decisions.value.filter((o) => o.decision === state && !retryable(inbox.detail!, o)).length
    return n ? `${n} ${state}` : ''
  }),
].filter(Boolean).join(' · '))
const pairs = computed(() => props.ops.flatMap((op) => (op.requires ?? [])
  .filter((id) => props.ops.some((o) => o.id === id))
  .map((id) => ({ from: id, to: op.id, label: `${opName(props.ops, id)} and ${opName(props.ops, op.id)} go together` }))))
const cableNote = computed(() => [...pairs.value.map((p) => `${p.label}.`), inboxNotes.cable].join(' '))
</script>

<template>
  <section class="ib-group" :aria-labelledby="`ib-group-${id}`">
    <div class="lib-h ib-group-head">
      <button :id="`ib-group-${id}`" type="button" class="ib-fold" :aria-expanded="!folded" :aria-controls="`ib-bank-${id}`" @click="emit('fold')">
        <Icon name="chevron" :class="{ folded }" />
        <span>{{ label }} <span v-if="summary" class="low">{{ summary }}</span></span>
      </button>
      <button v-if="eligible.length" type="button" class="rocker flat" role="switch" :aria-label="`All on in ${label}`" :aria-checked="on === eligible.length" @click="inbox.switchFields(eligible, on !== eligible.length, label)">
        <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span>
        <span class="rk-name">{{ on === eligible.length ? 'All on' : on ? 'Some on' : 'All off' }}</span>
      </button>
    </div>
    <div v-show="!folded" :id="`ib-bank-${id}`" ref="bank" class="ib-bank" :class="{ cabled: pairs.length, narrow: phone }">
      <LibTip v-if="pairs.length && !folded" title="Linked fields" :note="phone ? '' : cableNote"><InboxCable :pairs="pairs" :bank="bank" :narrow="phone" /></LibTip>
      <InboxOp v-for="op in ops" :key="op.id" :op="op" :phone="phone" @reveal="emit('reveal', $event)" />
    </div>
  </section>
</template>

<style scoped>
.rs .ib-group-head { top: var(--ib-tabs-height, 59px); gap: 4px 12px; padding-right: 10px; }
.rs .ib-fold { display: flex; align-items: center; gap: 6px; min-width: 0; min-height: 36px; padding: 0; border: 0; background: none; color: inherit; font: inherit; letter-spacing: inherit; text-align: left; text-transform: inherit; cursor: pointer; }
.rs .ib-fold .low { margin-left: 4px; }
.rs .ib-fold svg { flex: none; width: 16px; }
.rs .ib-fold .folded { transform: rotate(-90deg); }
.rs .ib-group-head .rocker { flex: none; min-height: 36px; padding: 0; }
.rs .ib-group-head .rk-name { min-width: 52px; font-size: 12px; }
.rs .ib-bank { position: relative; }
.rs .ib-bank.cabled { padding-left: 36px; }
.rs .ib-bank.cabled.narrow { padding-left: 26px; }
@media (max-width: 719px) {
  .rs .ib-fold, .rs .ib-group-head .rocker { min-height: 44px; }
  .rs .ib-fold .low { display: block; margin-left: 0; }
  .rs .ib-group-head { padding-left: 8px; }
}
</style>
