<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { Op } from '../api/agents'
import { coverSrc, decidedLine, decisionClass, decisionWord, fieldGroup, isLong, opLabel, trackLabel, wasLine } from '../lib/inbox'
import { count } from '../lib/format'
import { fieldNote, inboxNotes } from '../lib/notes'
import { held, retryable, selectable, useInbox } from '../stores/inbox'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import InboxCoverDialog from './InboxCoverDialog.vue'
import InboxValues from './InboxValues.vue'
import LibTip from './LibTip.vue'

const props = defineProps<{ op: Op; phone?: boolean }>()
const emit = defineEmits<{ reveal: [id: string] }>()
const inbox = useInbox()

const d = computed(() => inbox.detail!)
const decision = computed(() => d.value.ops.find((o) => o.id === props.op.id))
const pending = computed(() => decision.value?.decision === 'pending' && d.value.state !== 'withdrawn')
const retry = computed(() => decision.value && retryable(d.value, decision.value))
const canPick = computed(() => selectable(d.value, props.op.id))
const picked = computed(() => inbox.selected.includes(props.op.id))
const stale = computed(() => pending.value && !!d.value.live?.op_stale?.[props.op.id])
const conflict = computed(() => inbox.draftConflicts.find((c) => c.opId === props.op.id))
const edited = computed(() => !!inbox.drafts[props.op.id] && !conflict.value)
const label = computed(() => opLabel(props.op))
const note = computed(() => (!props.phone && props.op.type === 'tag' && props.op.field ? fieldNote(props.op.field) : ''))
const linked = computed(() => d.value.current.ops.filter((o) => props.op.requires?.includes(o.id) || o.requires?.includes(props.op.id)))
const crossLinks = computed(() => linked.value.filter((o) => fieldGroup(o) !== fieldGroup(props.op) && !held(d.value, o.id)))

const tag = computed(() => props.op.type === 'tag' ? props.op : null)
const tracks = computed(() => tag.value ? inbox.tracksOf(tag.value) : [])
const open = ref(false)
const split = ref(false)
const showMore = ref(false)
// Keep each editor's track membership until the reviewer explicitly regroups.
const regroupedValues = ref(new Map<number, string[]>())
function groupMatchingValues() {
  const op = tag.value
  if (!op) return
  regroupedValues.value = new Map(tracks.value.map((t) => [t.track_id, inbox.values(op, t.track_id)]))
  split.value = false
}
watch([() => d.value.head, edited], ([head, hasDraft], [oldHead]) => {
  if (!hasDraft || head !== oldHead) regroupedValues.value.clear()
})
const lists = computed(() => {
  const groups = new Map<string, { ids: number[]; values: string[] }>()
  const op = tag.value
  if (!op) return []
  for (const t of tracks.value) {
    const values = inbox.values(op, t.track_id)
    const key = split.value ? String(t.track_id) : JSON.stringify([regroupedValues.value.get(t.track_id) ?? t.values, values])
    const list = groups.get(key)
    if (list) list.ids.push(t.track_id)
    else groups.set(key, { ids: [t.track_id], values })
  }
  return [...groups.values()]
})
const spread = computed(() => split.value || lists.value.length > 1)
const visibleLists = computed(() => showMore.value ? lists.value : lists.value.slice(0, 5))
const dropped = computed(() => (tag.value?.tracks.length ?? 0) - tracks.value.length)
const scope = computed(() => {
  if (props.op.type === 'cover') {
    if (props.op.action === 'remove') return 'Folder and embedded covers'
    return inbox.everyTrack(props.op) ? 'Folder and every track' : 'Folder and tracks with a cover'
  }
  const ids = tracks.value.map((t) => t.track_id)
  return ids.length === 1 ? trackLabel(d.value, ids[0]!) : `${ids.length >= d.value.current.base.length ? 'All ' : ''}${count(ids.length, 'track')}`
})

const shown = (values: string[]) => values.length ? values.join(' / ') : 'Removed'
const proposed = computed(() => {
  if (props.op.type === 'cover') return props.op.action === 'remove' ? 'Remove artwork' : 'New cover'
  const value = shown(lists.value[0]?.values ?? [])
  return !split.value && lists.value.length > 1 ? `${value}, and ${count(lists.value.length - 1, 'other list')}` : split.value ? count(tracks.value.length, 'track') : value
})
const before = computed(() => d.value.live?.before?.[props.op.id])
const was = (ids: number[]) => wasLine(ids.map((id) => before.value?.[String(id)]))
const wasAll = computed(() => was(tracks.value.map((t) => t.track_id)))
const phoneState = computed(() => {
  if (!tag.value) return scope.value
  const value = spread.value ? count(lists.value.length, split.value ? 'track' : 'list') : proposed.value
  return wasAll.value ? `${value}, ${wasAll.value.replace(/^Was /, 'was ')}` : value
})
const blockLabel = (ids: number[]) => ids.length === 1 ? trackLabel(d.value, ids[0]!) : count(ids.length, 'track')

const opVal = ref<HTMLElement>()
const artPreview = ref<{ url: string; label: string } | null>(null)
const artTrigger = ref<HTMLElement | null>(null)

function openArtPreview(url: string, previewLabel: string, event: Event) {
  artTrigger.value = (event.currentTarget as HTMLElement) ?? null
  artPreview.value = { url, label: previewLabel }
}
function closeArtPreview() {
  artPreview.value = null
  void nextTick(() => artTrigger.value?.focus())
}
function leaveOut(trackId: number, listIndex: number) {
  inbox.dropTrack(props.op.id, trackId)
  void nextTick(() => {
    const groups = opVal.value?.querySelectorAll<HTMLElement>('.ib-val-group')
    const target = groups && groups.length > 0 ? groups[Math.min(listIndex, groups.length - 1)] : undefined
    const focusable = target?.querySelector<HTMLElement>('input, textarea, .lf-add, button') ?? opVal.value?.querySelector<HTMLElement>('input, textarea, button')
    focusable?.focus()
  })
}
</script>

<template>
  <div class="ib-op" :class="{ edited, off: canPick && !picked, phone, closed: !open }" :data-op="op.id">
    <div class="ib-op-row">
      <div class="ib-op-sw">
        <button v-if="canPick" type="button" class="rocker flat ib-paddle" role="switch" :aria-checked="picked" :aria-label="label" @click="inbox.toggle(op.id)">
          <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span>
        </button>
        <button type="button" class="ib-label rk-t" :aria-expanded="open" @click="open = !open">
          <Icon name="chevron" :class="{ folded: !open }" />
          <span class="col g4 grow">
            <span class="rk-name"><LibTip :title="label" :note="note">{{ label }}</LibTip></span>
            <span class="rk-state sm ell" :title="phone && !open ? phoneState : scope">{{ phone && !open ? phoneState : scope }}</span>
            <span v-if="phone && decision && !pending" class="sm" :class="retry ? 'bad-text' : 't2'">{{ retry ? 'Retry available' : decisionWord(decision.decision) }}</span>
            <span v-else-if="phone && stale" class="sm t2">Changed since proposed</span>
          </span>
        </button>
        <span v-if="linked.length" class="sr">Goes with {{ linked.map(opLabel).join(', ') }}. They switch on and off together.</span>
      </div>
      <div v-if="!phone" class="ib-op-summary col g4">
        <span class="ell" :title="proposed">{{ proposed }}</span>
        <span v-if="wasAll" class="lf-was ell" :title="wasAll">{{ wasAll }}</span>
      </div>
      <div class="ib-op-side">
        <span v-if="edited" class="led stg" role="img" aria-label="Edited, not saved"></span>
        <span v-if="!phone && stale" class="pill partial">Changed since proposed</span>
        <span v-if="!phone && decision && !pending" class="pill" :class="retry ? 'failed' : decisionClass(decision.decision)">{{ retry ? 'Retry available' : decisionWord(decision.decision) }}</span>
      </div>
    </div>

    <div v-if="open" ref="opVal" class="ib-op-val col g8">
      <p v-if="conflict" class="bad-text sm" role="alert">
        Your draft for {{ conflict.originalLabel }} no longer matches this field ({{ conflict.reason }}).
        <template v-if="conflict.text"> Draft: <span class="mono">{{ conflict.text }}</span>.</template>
        <button type="button" class="link sm" @click="inbox.discard(conflict.opId)">Discard {{ conflict.originalLabel }} draft</button>
      </p>
      <p v-if="linked.length" class="t2 sm">
        Goes with <template v-for="(other, i) in linked" :key="other.id">{{ i ? ', ' : '' }}<button v-if="crossLinks.includes(other)" type="button" class="link sm" @click="emit('reveal', other.id)">{{ opLabel(other) }}</button><template v-else>{{ opLabel(other) }}</template></template>.
      </p>
      <p v-if="op.type === 'tag' && op.anchor" class="t2 sm">MusicBrainz release <span class="mono">{{ op.anchor }}</span></p>
      <template v-if="tag">
        <div v-if="pending && (tracks.length > 1 || split)" class="line g8 wrap">
          <button v-if="!split" type="button" class="key sm flat" @click="split = true"><LibTip title="Per track" :note="phone ? '' : inboxNotes.perTrack">Per track</LibTip></button>
          <button v-else type="button" class="key sm flat" @click="groupMatchingValues">Group matching values</button>
        </div>
        <div v-for="(list, i) in visibleLists" :key="list.ids[0]" class="col g4 ib-val-group" :class="{ 'ib-track': spread }">
          <div v-if="spread" class="line g8 ib-track-head">
            <span class="lbl-f grow ib-wrap" :title="list.ids.map((id) => trackLabel(d, id)).join(', ')">{{ blockLabel(list.ids) }}</span>
            <button v-if="pending && list.ids.length === 1 && tracks.length > 1" type="button" class="link sm" @click="leaveOut(list.ids[0]!, i)">Leave out</button>
          </div>
          <InboxValues v-if="pending && !conflict" :values="list.values" :label="spread ? `${label}, ${blockLabel(list.ids)}` : label" :long="isLong(op)" @update="(v) => inbox.setValues(op.id, list.ids, v)" />
          <p v-else class="ib-value">{{ shown(list.values) }}</p>
          <span v-if="was(list.ids)" class="lf-was ib-wrap" :title="was(list.ids)">{{ was(list.ids) }}</span>
        </div>
      <button v-if="lists.length > visibleLists.length" type="button" class="link sm" @click="showMore = true">Show {{ count(lists.length - visibleLists.length, split ? 'more track' : 'more list') }}</button>
      </template>
      <template v-else-if="op.type === 'cover' && op.action === 'replace'">
        <button v-if="op.image" type="button" class="ib-cover-btn" :aria-label="`Inspect ${label}, after`" @click="openArtPreview(coverSrc(op.image), `${label}, after`, $event)">
          <Cover :url="coverSrc(op.image)" :size="72" />
        </button>
        <button v-if="pending && !conflict" type="button" class="rocker flat" role="switch" :aria-checked="inbox.everyTrack(op)" @click="inbox.setEveryTrack(op.id, !inbox.everyTrack(op))">
          <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span><span class="rk-name">Embed in every track</span>
        </button>
      </template>
      <p v-if="decision && decidedLine(decision)" class="t2 sm">{{ decidedLine(decision) }}. <template v-if="retry">The write failed before any files were written. You can retry this field.</template></p>
      <p v-if="dropped && pending" class="t2 sm">{{ count(dropped, 'track') }} left out.</p>
      <button v-if="edited && pending" type="button" class="link sm" @click="inbox.discard(op.id)">Undo my edits to {{ label }}</button>
    </div>
    <InboxCoverDialog v-if="artPreview" :url="artPreview.url" :label="artPreview.label" @close="closeArtPreview" />
  </div>
</template>

<style scoped>
.rs .ib-op { padding: 0 14px; border-bottom: 1px solid var(--line); scroll-margin-top: calc(var(--ib-tabs-height, 59px) + 48px); }
.rs .ib-op.edited { box-shadow: inset 3px 0 0 var(--sun); }
.rs .ib-op.off .ib-op-summary { color: var(--text-2); }
.rs .ib-op-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr) auto; gap: 8px 12px; align-items: center; min-height: 63px; }
.rs .ib-op-sw { display: flex; align-items: center; gap: 8px; min-width: 0; }
.rs .ib-paddle { flex: none; min-height: 44px; padding: 0; }
.rs .ib-label { display: flex; flex: 1 1 auto; flex-direction: row; align-items: center; gap: 6px; min-width: 0; min-height: 44px; padding: 0; border: 0; background: none; color: var(--text); text-align: left; cursor: pointer; }
.rs .ib-label > span { min-width: 0; }
.rs .ib-label svg { flex: none; width: 15px; }
.rs .ib-label .folded { transform: rotate(-90deg); }
.rs .ib-label .rk-name { white-space: normal; overflow-wrap: anywhere; }
.rs .ib-label .rk-state { max-width: 100%; }
.rs .ib-op-summary { min-width: 0; }
.rs .ib-op-side { display: flex; flex-wrap: wrap; justify-content: flex-end; align-items: center; gap: 6px; max-width: 115px; }
.rs .ib-op-side:empty { display: none; }
.rs .ib-op-side .pill { white-space: normal; }
.rs .ib-op-val { align-items: flex-start; padding-bottom: 12px; min-width: 0; overflow-wrap: anywhere; }
.rs .ib-op-val > div { width: 100%; min-width: 0; }
.rs .ib-track { padding-left: 10px; border-left: 2px solid var(--line); }
.rs .ib-track-head { align-items: flex-start; }
.rs .ib-track-head button { flex: none; }
.rs .ib-wrap { white-space: pre-wrap; overflow-wrap: anywhere; }
.rs .ib-value { white-space: pre-wrap; }
.rs .ib-cover-btn { display: inline-flex; padding: 0; border: 0; border-radius: 3px; background: none; cursor: zoom-in; }
.rs .ib-cover-btn:hover .cv { filter: brightness(1.08); }
.rs .ib-op.phone { padding: 0 10px; }
.rs .ib-op.phone .ib-op-row { grid-template-columns: minmax(0, 1fr) auto; min-height: 71px; }
.rs .ib-op.phone .rk-state { white-space: nowrap; }
</style>
