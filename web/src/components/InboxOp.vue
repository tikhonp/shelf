<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Op, TagOp } from '../api/agents'
import { decidedLine, decisionClass, decisionWord, isLong, opLabel, opName, trackLabel } from '../lib/inbox'
import { selectable, useInbox } from '../stores/inbox'
import InboxValues from './InboxValues.vue'

const props = defineProps<{ op: Op }>()
const inbox = useInbox()

const d = computed(() => inbox.detail!)
const decision = computed(() => d.value.ops.find((o) => o.id === props.op.id))
const pending = computed(() => decision.value?.decision === 'pending' && d.value.state !== 'withdrawn')
const canPick = computed(() => selectable(d.value, props.op.id))
const picked = computed(() => inbox.selected.includes(props.op.id))
const stale = computed(() => !!d.value.live?.op_stale?.[props.op.id])
const edited = computed(() => !!inbox.drafts[props.op.id])
const label = computed(() => opLabel(props.op))
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
</script>

<template>
  <div class="ib-op" :class="{ picked, edited, off: canPick && !picked }">
    <div class="ib-op-h">
      <button v-if="canPick" type="button" class="rocker flat" role="switch" :aria-checked="picked" @click="inbox.toggle(op.id)">
        <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span>
        <span class="rk-t"><span class="rk-name">{{ label }}</span><span class="rk-state sm">{{ scope }}</span></span>
      </button>
      <span v-else class="col g4"><span class="bold">{{ label }}</span><span class="t2 sm">{{ scope }}</span></span>
      <span v-if="op.type === 'tag' && op.tag_key && !op.field" class="mono t2">{{ op.tag_key }}</span>
      <span class="grow"></span>
      <span v-if="edited" class="led stg" role="img" aria-label="Edited, not saved"></span>
      <span v-if="stale" class="pill partial">Changed since proposed</span>
      <span v-if="decision && !pending" class="pill" :class="decisionClass(decision.decision)">{{ decisionWord(decision.decision) }}</span>
    </div>
    <p v-if="decision && decidedLine(decision)" class="t2 sm">{{ decidedLine(decision) }}</p>
    <p v-if="goesWith.length" class="t2 sm ib-cable"><span class="ib-plug" aria-hidden="true"></span>Goes with {{ goesWith.join(', ') }}. They switch on and off together.</p>
    <p v-if="op.type === 'tag' && op.anchor" class="t2 sm">MusicBrainz release {{ op.anchor }}</p>

    <template v-if="tag">
      <template v-if="pending">
        <template v-if="!perTrack">
          <InboxValues :values="lists[0] ?? []" :label="label" :long="isLong(op)" @update="setAll" />
          <button v-if="tracks.length > 1" type="button" class="key sm self" @click="split = true">Per track</button>
        </template>
        <template v-else>
          <div v-for="(t, i) in tracks" :key="t.track_id" class="col g4 ib-track">
            <div class="line g8"><span class="lbl-f grow ell">{{ trackLabel(d, t.track_id) }}</span><button v-if="tracks.length > 1" type="button" class="link sm" @click="inbox.dropTrack(op.id, t.track_id)">Leave out</button></div>
            <InboxValues :values="lists[i] ?? []" :label="`${label}, ${trackLabel(d, t.track_id)}`" :long="isLong(op)" @update="(v) => setOne(t.track_id, v)" />
          </div>
          <button v-if="shared && split" type="button" class="key sm self" @click="split = false">All tracks together</button>
        </template>
        <p v-if="dropped" class="t2 sm">{{ dropped }} {{ dropped === 1 ? 'track' : 'tracks' }} left out.</p>
        <button v-if="edited" type="button" class="link sm self" @click="inbox.discard(op.id)">Undo my edits to {{ label }}</button>
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
</template>

<style scoped>
.rs .ib-op { display: flex; flex-direction: column; gap: 8px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.rs .ib-op.edited { box-shadow: inset 3px 0 0 var(--sun); }
/* A field that is off stays editable but reads as left out of Accept. */
.rs .ib-op.off :deep(.lib-in) { opacity: .6; }
.rs .ib-op-h { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; min-width: 0; }
.rs .ib-op-h .rk-name { white-space: normal; }
.rs .ib-track { padding-left: 10px; border-left: 2px solid var(--line); }
.rs .ib-cable { display: flex; align-items: center; gap: 8px; }
/* A plug mark on a field that requires another one, in place of the design's drawn cable. */
.rs .ib-plug { flex: none; width: 22px; height: 10px; border: 2px solid var(--sun); border-left: 0; border-radius: 0 6px 6px 0; }
.rs .self { align-self: flex-start; }
</style>
