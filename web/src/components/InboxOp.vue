<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Op, TagOp } from '../api/agents'
import { decidedLine, decisionClass, decisionWord, isLong, opLabel, opName, trackLabel, wasLine } from '../lib/inbox'
import { fieldNote, inboxNotes } from '../lib/notes'
import { selectable, useInbox } from '../stores/inbox'
import InboxValues from './InboxValues.vue'
import LibTip from './LibTip.vue'

const props = defineProps<{ op: Op; phone?: boolean }>()
const inbox = useInbox()

const d = computed(() => inbox.detail!)
const decision = computed(() => d.value.ops.find((o) => o.id === props.op.id))
const pending = computed(() => decision.value?.decision === 'pending' && d.value.state !== 'withdrawn')
const canPick = computed(() => selectable(d.value, props.op.id))
const picked = computed(() => inbox.selected.includes(props.op.id))
const stale = computed(() => !!d.value.live?.op_stale?.[props.op.id])
const edited = computed(() => !!inbox.drafts[props.op.id])
const label = computed(() => opLabel(props.op))
const note = computed(() => (props.op.type === 'tag' && props.op.field ? fieldNote(props.op.field) : ''))
const goesWith = computed(() => (props.op.requires ?? []).map((id) => opName(d.value.current.ops, id)))

const tag = computed(() => (props.op.type === 'tag' ? (props.op as TagOp) : null))
const tracks = computed(() => (tag.value ? inbox.tracksOf(tag.value) : []))
const lists = computed(() => tracks.value.map((t) => inbox.values(tag.value!, t.track_id)))
// One editor serves every track while they share their values, and the reviewer can split it per track.
const split = ref(false)
const shared = computed(() => lists.value.every((l) => l.length === lists.value[0]!.length && l.every((v, i) => v === lists.value[0]![i])))
const perTrack = computed(() => split.value || !shared.value)
const dropped = computed(() => (tag.value ? tag.value.tracks.length - tracks.value.length : 0))
const scope = computed(() => {
  if (!tag.value) return props.op.type === 'cover' && props.op.every_track ? 'Folder and every track' : 'Folder cover'
  const n = tracks.value.length
  if (n === 1) return trackLabel(d.value, tracks.value[0]!.track_id)
  return n >= d.value.current.base.length ? `All ${n} tracks` : `${n} tracks`
})

const setAll = (values: string[]) => inbox.setValues(props.op.id, tracks.value.map((t) => t.track_id), values)
const setOne = (trackId: number, values: string[]) => inbox.setValues(props.op.id, [trackId], values)
const shown = (values: string[]) => (values.length ? values.join(' / ') : 'Removed')

// Was is what the files hold now, from the live part of the detail.
const before = computed(() => d.value.live?.before?.[props.op.id])
const was = (trackIds: number[]) => wasLine(trackIds.map((id) => before.value?.[String(id)]))
const wasAll = computed(() => was(tracks.value.map((t) => t.track_id)))
// On the phone a row shows the rocker with the new value, and pressing the label opens the field.
const open = ref(false)
const phoneState = computed(() => {
  if (!tag.value) return scope.value
  const value = perTrack.value ? `${tracks.value.length} values` : shown(lists.value[0] ?? [])
  const w = wasAll.value.replace(/^Was /, 'was ')
  return w ? `${value}, ${w}` : value
})
const editing = computed(() => !props.phone || open.value)
</script>

<template>
  <div class="ib-op" :class="{ picked, edited, off: canPick && !picked, phone }" :data-op="op.id">
    <div class="ib-op-row">
      <div class="ib-op-sw">
        <template v-if="canPick && phone">
          <button type="button" class="rocker flat ib-paddle" role="switch" :aria-checked="picked" :aria-label="label" @click="inbox.toggle(op.id)">
            <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span>
          </button>
          <button type="button" class="ib-label rk-t" :aria-expanded="open" @click="open = !open"><span class="rk-name">{{ label }}</span><span class="rk-state sm">{{ phoneState }}</span></button>
        </template>
        <button v-else-if="canPick" type="button" class="rocker flat" role="switch" :aria-checked="picked" @click="inbox.toggle(op.id)">
          <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span>
          <span class="rk-t"><span class="rk-name"><LibTip :title="label" :note="note">{{ label }}</LibTip></span><span class="rk-state sm">{{ scope }}</span></span>
        </button>
        <span v-else class="col g4"><span class="bold"><LibTip :title="label" :note="phone ? '' : note">{{ label }}</LibTip></span><span class="t2 sm">{{ scope }}</span></span>
        <span v-if="goesWith.length" class="sr">Goes with {{ goesWith.join(', ') }}. They switch on and off together.</span>
      </div>

      <div v-if="editing" class="ib-op-val">
        <template v-if="tag">
          <template v-if="pending">
            <template v-if="!perTrack">
              <InboxValues :values="lists[0] ?? []" :label="label" :long="isLong(op)" @update="setAll" />
              <span v-if="wasAll" class="lf-was ell" :title="wasAll">{{ wasAll }}</span>
            </template>
            <template v-else>
              <div v-for="(t, i) in tracks" :key="t.track_id" class="col g4 ib-track">
                <div class="line g8"><span class="lbl-f grow ell">{{ trackLabel(d, t.track_id) }}</span><button v-if="tracks.length > 1" type="button" class="link sm" @click="inbox.dropTrack(op.id, t.track_id)">Leave out</button></div>
                <InboxValues :values="lists[i] ?? []" :label="`${label}, ${trackLabel(d, t.track_id)}`" :long="isLong(op)" @update="(v) => setOne(t.track_id, v)" />
                <span v-if="was([t.track_id])" class="lf-was ell">{{ was([t.track_id]) }}</span>
              </div>
            </template>
          </template>
          <dl v-else class="meta">
            <template v-for="t in tag.tracks" :key="t.track_id"><dt>{{ trackLabel(d, t.track_id) }}</dt><dd>{{ shown(t.values) }}</dd></template>
          </dl>
        </template>
        <template v-else-if="op.type === 'cover'">
          <button v-if="pending && op.action === 'replace'" type="button" class="rocker flat self" role="switch" :aria-checked="inbox.everyTrack(op)" @click="inbox.setEveryTrack(op.id, !inbox.everyTrack(op))">
            <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span><span class="rk-name">Embed in every track</span>
          </button>
          <p v-else-if="op.action === 'replace'" class="t2 sm">{{ op.every_track ? 'Embedded in every track too.' : 'Folder cover, and tracks that already embed one.' }}</p>
        </template>
      </div>

      <div class="ib-op-side">
        <span v-if="op.type === 'tag' && op.tag_key && !op.field" class="mono t2">{{ op.tag_key }}</span>
        <span v-if="edited" class="led stg" role="img" aria-label="Edited, not saved"></span>
        <span v-if="stale" class="pill partial">Changed since proposed</span>
        <span v-if="decision && !pending" class="pill" :class="decisionClass(decision.decision)">{{ decisionWord(decision.decision) }}</span>
        <template v-if="tag && pending && editing">
          <button v-if="!perTrack && tracks.length > 1" type="button" class="key sm flat" @click="split = true"><LibTip title="Per track" :note="phone ? '' : inboxNotes.perTrack">Per track</LibTip></button>
          <button v-else-if="shared && split" type="button" class="key sm flat" @click="split = false">All tracks together</button>
        </template>
      </div>
    </div>
    <p v-if="decision && decidedLine(decision)" class="t2 sm">{{ decidedLine(decision) }}</p>
    <p v-if="op.type === 'tag' && op.anchor" class="t2 sm">MusicBrainz release {{ op.anchor }}</p>
    <p v-if="dropped && pending" class="t2 sm">{{ dropped }} {{ dropped === 1 ? 'track' : 'tracks' }} left out.</p>
    <button v-if="edited && pending" type="button" class="link sm self" @click="inbox.discard(op.id)">Undo my edits to {{ label }}</button>
  </div>
</template>

<style scoped>
.rs .ib-op { display: flex; flex-direction: column; gap: 8px; padding: 12px 14px; border-bottom: 1px solid var(--line); }
.rs .ib-op.edited { box-shadow: inset 3px 0 0 var(--sun); }
/* A field that is off stays editable but reads as left out of Accept. */
.rs .ib-op.off .ib-op-val { opacity: .6; }
/* One row per field, as in the bulk run: the rocker, the value with what the files hold now, then the keys. */
.rs .ib-op-row { display: grid; grid-template-columns: minmax(180px, 250px) minmax(0, 1fr) auto; gap: 8px 16px; align-items: center; }
.rs .ib-op-sw { min-width: 0; }
.rs .ib-op-sw .rk-name { white-space: normal; }
.rs .ib-op-val { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.rs .ib-op-side { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 8px; }
.rs .ib-op-side:empty { display: none; }
.rs .ib-track { padding-left: 10px; border-left: 2px solid var(--line); }
.rs .self { align-self: flex-start; }
@media (max-width: 999px) {
  .rs .ib-op-row { grid-template-columns: minmax(0, 1fr) auto; }
  .rs .ib-op-val { grid-column: 1 / -1; grid-row: 2; }
}
.rs .ib-op.phone .ib-op-sw { display: flex; align-items: center; gap: 10px; }
.rs .ib-paddle { flex: none; min-height: 44px; }
.rs .ib-label { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; min-width: 0; min-height: 44px; padding: 0; border: 0; background: none; color: var(--text); text-align: left; cursor: pointer; }
/* The state can quote a whole value, such as a MusicBrainz ID, so it wraps instead of widening the page. */
.rs .ib-label .rk-state { white-space: normal; overflow-wrap: anywhere; }
</style>
