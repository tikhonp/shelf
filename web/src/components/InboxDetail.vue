<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import type { Acceptance, Evidence } from '../api/agents'
import { useNow } from '../lib/clock'
import { ago, count } from '../lib/format'
import { eventLine, fieldGroup, fieldGroups, opName, stateClass, stateWord } from '../lib/inbox'
import { inboxNotes, reviewNote } from '../lib/notes'
import { fieldLabel } from '../lib/tags'
import { useHelp } from '../stores/help'
import { held, ordered, selectable, useInbox } from '../stores/inbox'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import InboxAcceptance from './InboxAcceptance.vue'
import InboxBar from './InboxBar.vue'
import InboxDiff from './InboxDiff.vue'
import InboxGroup from './InboxGroup.vue'
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
const tabs = [
  { id: 'fields', label: 'Fields' },
  { id: 'brief', label: 'Brief' },
  { id: 'files', label: 'Files' },
  { id: 'talkback', label: 'Talkback' },
] as const
type Tab = typeof tabs[number]['id']
const tab = ref<Tab>('fields')
const body = ref<HTMLElement>()
const tabBar = ref<HTMLElement>()
const fileChoice = ref<number | 'planned'>()
const folded = ref<Partial<Record<keyof typeof fieldGroups, boolean>>>({ ids: true })

watch(tabBar, (el, _, onCleanup) => {
  if (!el) return
  const measure = () => body.value?.style.setProperty('--ib-tabs-height', `${el.offsetHeight}px`)
  measure()
  if (typeof ResizeObserver !== 'undefined') {
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    onCleanup(() => observer.disconnect())
  }
}, { flush: 'post' })

function chooseTab(next: Tab, focus = false) {
  const scroller = body.value
  const bar = tabBar.value
  const pinned = !!(scroller?.scrollTop && bar && bar.getBoundingClientRect().top <= scroller.getBoundingClientRect().top + 1)
  const changed = tab.value !== next
  tab.value = next
  void nextTick(() => {
    if (changed && scroller && bar) {
      scroller.scrollTop = 0
      if (pinned) scroller.scrollTop = bar.getBoundingClientRect().top - scroller.getBoundingClientRect().top
    }
    if (focus) bar?.querySelector<HTMLElement>(`#tab-${next}`)?.focus({ preventScroll: true })
  })
}

function tabKey(event: KeyboardEvent, index: number) {
  const n = tabs.length
  const next = { ArrowRight: (index + 1) % n, ArrowLeft: (index + n - 1) % n, Home: 0, End: n - 1 }[event.key]
  if (next === undefined) return
  event.preventDefault()
  chooseTab(tabs[next]!.id, true)
}

function openFiles(acceptanceId?: number) {
  fileChoice.value = acceptanceId ?? 'planned'
  chooseTab('files', true)
}
defineExpose({ openFiles })

const albumId = computed(() => d.value?.album.current_id ?? 0)
const cover = computed(() => (albumId.value ? `/api/library/albums/${albumId.value}/cover` : ''))
const head = computed(() => d.value?.revisions.find((r) => r.revision === d.value?.head))
const hasPending = computed(() => (d.value?.summary.pending ?? 0) > 0 && d.value?.state !== 'withdrawn')
// A field the files already hold writes nothing, so the Inbox hides it.
const ops = computed(() => (d.value ? ordered(d.value.current.ops).filter((o) => !held(d.value!, o.id)) : []))
const hidden = computed(() => (d.value ? d.value.current.ops.filter((o) => held(d.value!, o.id)).length : 0))
const name = (id: string) => opName(d.value?.current.ops ?? [], id)
const superseded = computed(() => d.value?.ops.filter((o) => !d.value!.current.ops.some((c) => c.id === o.id)) ?? [])
const groups = computed(() => Object.entries(fieldGroups).flatMap(([id, label]) => {
  const members = ops.value.filter((op) => fieldGroup(op) === id)
  return members.length ? [{ id: id as keyof typeof fieldGroups, label, ops: members }] : []
}))
const eligible = computed(() => ops.value.filter((op) => d.value && selectable(d.value, op.id)))
const anyFolded = computed(() => groups.value.some((group) => folded.value[group.id]))
function foldAll() {
  folded.value = Object.fromEntries(groups.value.map((group) => [group.id, !anyFolded.value]))
}
async function reveal(id: string) {
  const op = ops.value.find((op) => op.id === id)
  if (!op) return
  folded.value[fieldGroup(op)] = false
  await nextTick()
  const row = body.value?.querySelector<HTMLElement>(`[data-op="${id}"]`)
  const button = row?.querySelector<HTMLElement>('.ib-label')
  if (button?.getAttribute('aria-expanded') === 'false') button.click()
  await nextTick()
  button?.focus({ preventScroll: true })
  row?.scrollIntoView?.({ block: 'start' })
}

const byLine = computed(() => {
  const v = d.value
  if (!v) return ''
  const when = head.value ? ` · ${ago(head.value.created_at, now.value)}` : ''
  return `${v.connection.name}${when} · `
})
const editedBy = computed(() => (head.value && head.value.author !== d.value?.connection.name ? `, edited by ${head.value.author}` : ''))
function toggleNote() {
  help.note = help.note ? null : { title: 'Review', text: reviewNote(d.value?.current.confidence, ops.value.some((op) => op.requires?.length)) }
}

const written = (a: Acceptance) => !!a.preview && (a.via_save || a.outcome.state === 'done')
const records = computed(() => d.value?.acceptances.filter((a) => written(a) || a.id === fileChoice.value).slice().reverse() ?? [])
const activeFileChoice = computed(() => {
  if (inbox.reviewRequired) return 'planned'
  return fileChoice.value ?? (eligible.value.length || !records.value.length ? 'planned' : records.value[0]!.id)
})
const record = computed(() => d.value?.acceptances.find((a) => a.id === activeFileChoice.value))
const filePreview = computed(() => record.value ? (written(record.value) ? record.value.preview : null) : inbox.preview)
const filesTitle = computed(() => record.value ? (record.value.via_save ? 'Proposal fields written by Save' : 'What Accept wrote') : 'What Accept would write')
const filesNote = computed(() => record.value ? `Revision ${record.value.revision} · ${ago(record.value.created_at, now.value)}` : `Saved revision ${d.value?.head}`)
const acceptanceEvents = computed(() => new Map(d.value?.events.filter((e) => e.kind === 'accepted').map((e) => [e.acceptance_id ?? Number(e.data?.acceptance_id), e.id])))
const thread = computed(() => (d.value?.events ?? []).map((event) => ({
  event,
  acceptance: d.value?.acceptances.find((a) => acceptanceEvents.value.get(a.id) === event.id),
})))
const otherAcceptances = computed(() => d.value?.acceptances.filter((a) => !acceptanceEvents.value.has(a.id)) ?? [])
watch(() => [...inbox.selected].sort().join(','), () => { fileChoice.value = undefined })

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
  const ok = hasPending.value && asking.value ? await inbox.requestChanges(reply.value) : await inbox.comment(reply.value)
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
    <div v-if="d" ref="body" class="vscroll ib-body">
      <header class="ib-head pad">
        <Cover :url="cover" :size="phone ? 64 : 84" />
        <div class="col g4 grow">
          <h3 class="ib-title clamp2">{{ d.current.title }}</h3>
          <p class="t2 sm">{{ byLine }}<LibTip title="Revision" :note="tip(inboxNotes.revision)">revision {{ d.head }}</LibTip>{{ editedBy }}</p>
          <p class="t2 sm mono ell" :title="d.album.folder">{{ d.album.folder }}</p>
          <div v-if="d.state !== 'pending' || d.changes_requested" class="line g8"><span class="pill" :class="stateClass(d.state, d.changes_requested)">{{ stateWord(d) }}</span></div>
        </div>
        <div v-if="d.current.confidence != null" class="lcd ib-conf" role="img" :aria-label="`Confidence ${d.current.confidence}%`">
          <span class="lcd-l">SURE</span><span class="lcd-big num"><LibTip title="Confidence" :note="tip(inboxNotes.confidence)">{{ d.current.confidence }}%</LibTip></span>
        </div>
      </header>

      <div class="note ib-why col g8" v-if="tab !== 'brief'" role="note" aria-label="The agent's explanation">
        <p class="ib-reason clamp2">{{ d.current.reason }}</p>
        <p v-if="d.current.uncertainties" class="t2 clamp2"><strong>Not sure</strong> · {{ d.current.uncertainties }}</p>
        <button type="button" class="link sm ib-self" @click="chooseTab('brief', true)">Read the brief<template v-if="d.current.evidence.length"> and {{ count(d.current.evidence.length, 'evidence item') }}</template></button>
      </div>

      <div ref="tabBar" class="ib-sec ib-tabs">
        <div class="segk" :class="{ block: phone }" role="tablist" aria-label="Proposal">
          <button v-for="(t, i) in tabs" :id="`tab-${t.id}`" :key="t.id" type="button" role="tab" :aria-controls="`panel-${t.id}`" :aria-selected="tab === t.id" :tabindex="tab === t.id ? 0 : -1" @click="chooseTab(t.id)" @keydown="tabKey($event, i)">
            {{ t.label }}<span v-if="t.id === 'files' && inbox.reviewRequired" class="led stg" role="img" aria-label="Updated preview"></span>
          </button>
        </div>
        <div v-if="!phone && tab === 'fields'" class="line g12 sm wrap ib-tools">
          <span v-if="eligible.length" class="t2">{{ inbox.shown.length }} of {{ eligible.length }} on</span>
          <button v-if="eligible.length" type="button" class="link sm" @click="inbox.selectAll(true)">All on</button>
          <button v-if="eligible.length" type="button" class="link sm" @click="inbox.selectAll(false)">All off</button>
          <button v-if="groups.length" type="button" class="link sm" @click="foldAll">{{ anyFolded ? 'Expand groups' : 'Collapse groups' }}</button>
        </div>
      </div>

      <div v-show="tab === 'fields'" id="panel-fields" role="tabpanel" aria-labelledby="tab-fields">
        <div v-if="phone && eligible.length" class="line g12 sm ib-phone-tools">
          <span class="t2 grow">{{ inbox.shown.length }} of {{ eligible.length }} on</span>
          <button type="button" class="link sm" @click="inbox.selectAll(true)">All on</button>
          <button type="button" class="link sm" @click="inbox.selectAll(false)">All off</button>
        </div>
        <p v-if="cascadeText" class="t2 sm pad ib-moved" role="status">{{ cascadeText }}</p>
        <InboxGroup v-for="group in groups" :key="group.id" v-bind="group" :phone="phone" :folded="!!folded[group.id]" @fold="folded[group.id] = !folded[group.id]" @reveal="reveal" />
        <p v-if="hidden" class="t2 sm pad ib-held">{{ hidden === 1 ? '1 field the files already hold is hidden' : `${hidden} fields the files already hold are hidden` }}. Accept marks {{ hidden === 1 ? 'it' : 'them' }} accepted too.</p>
        <p v-if="superseded.length" class="t2 sm pad">Earlier fields no longer in this revision include {{ superseded.map((o) => name(o.id)).join(', ') }}.</p>
      </div>

      <div v-show="tab === 'brief'" id="panel-brief" role="tabpanel" aria-labelledby="tab-brief">
        <div class="pad col g8">
          <h3 class="ib-title">{{ d.current.title }}</h3>
          <p class="ib-reason">{{ d.current.reason }}</p>
        </div>
        <p v-if="d.current.uncertainties" class="note ib-why ib-reason"><strong><LibTip title="Not sure" :note="tip(inboxNotes.notSure)">Not sure</LibTip></strong> · {{ d.current.uncertainties }}</p>
        <template v-if="d.current.evidence.length">
          <div class="ib-sec"><h3>Evidence</h3></div>
          <div class="ib-tapes pad" aria-label="Evidence">
            <component :is="href(e) ? 'a' : 'span'" v-for="(e, i) in d.current.evidence" :key="i" class="sug" :class="{ tape: !href(e) }" :href="href(e) || undefined" :target="href(e) ? '_blank' : undefined" :rel="href(e) ? 'noopener noreferrer' : undefined">
              <span class="sug-v"><LibTip title="Evidence" :note="i === 0 ? tip(inboxNotes.evidence) : ''">{{ tapeValue(e) }}</LibTip></span>
              <span class="sug-s">{{ tapeSource(e) }}<span v-if="href(e)" aria-hidden="true"> ↗</span></span>
            </component>
          </div>
        </template>
        <div class="ib-sec"><h3>About this proposal</h3></div>
        <dl class="meta pad">
          <dt>Agent</dt><dd>{{ d.connection.name }}</dd>
          <dt>Revision</dt><dd>{{ d.head }}<template v-if="head"> · {{ ago(head.created_at, now) }}{{ editedBy }}</template></dd>
          <template v-if="d.current.confidence != null"><dt>Confidence</dt><dd>{{ d.current.confidence }}%</dd></template>
          <dt>Folder</dt><dd class="mono">{{ d.album.folder }}</dd>
          <template v-if="d.group"><dt>Group</dt><dd>{{ d.group }}. Each album is reviewed and written on its own.</dd></template>
          <template v-if="d.follow_up_of"><dt>Follows up</dt><dd><RouterLink :to="`/inbox/${d.follow_up_of}`">Proposal {{ d.follow_up_of }}</RouterLink></dd></template>
        </dl>
      </div>

      <div v-show="tab === 'files'" id="panel-files" role="tabpanel" aria-labelledby="tab-files">
        <label v-if="records.length && !inbox.reviewRequired" class="pad line g8 wrap ib-sel">
          <span>Show</span>
          <select :value="activeFileChoice" @change="fileChoice = ($event.target as HTMLSelectElement).value === 'planned' ? 'planned' : Number(($event.target as HTMLSelectElement).value)">
            <option v-if="eligible.length || activeFileChoice === 'planned'" value="planned">Planned writes</option>
            <option v-for="a in records" :key="a.id" :value="a.id">Change {{ a.change_id }} · Revision {{ a.revision }}</option>
          </select>
        </label>
        <p v-if="!record && inbox.dirty" class="t2 sm pad">Save your edited values to update this preview.</p>
        <template v-if="filePreview">
          <InboxDiff :key="activeFileChoice" :preview="filePreview" :title="filesTitle" :note="filesNote" :fields="record ? record.ops.length : inbox.shown.length" :phone="phone" :empty="record ? 'No file changes were recorded.' : 'The files already match the selected values.'" />
          <div v-if="inbox.reviewRequired && !record" class="pad">
            <button type="button" class="key sm" :disabled="inbox.dirty || inbox.busy" @click="inbox.reviewPreview(filePreview.digest)">I reviewed the updated changes</button>
          </div>
        </template>
        <div v-else-if="record" class="pad">
          <InboxAcceptance :acceptance="record" :album-id="albumId" :phone="phone" />
        </div>
        <p v-else-if="inbox.previewing || inbox.detailRefreshing" class="t2 sm pad" role="status">Checking the selected fields against the files...</p>
        <p v-else-if="inbox.shown.length" class="t2 sm pad">The selected preview is unavailable.</p>
        <p v-else-if="eligible.length" class="t2 sm pad">Switch on fields to see what Accept would write.</p>
        <p v-else class="t2 sm pad">No completed writes to show. See Talkback for review outcomes.</p>
        <InboxDiff v-if="!albumId && !filePreview && !record" :preview="d.current.preview" title="As proposed" note="Recorded with the revision" :phone="phone" />
      </div>

      <div v-show="tab === 'talkback'" id="panel-talkback" role="tabpanel" aria-labelledby="tab-talkback">
        <div class="ib-sec"><h3><LibTip title="Talkback" :note="tip(inboxNotes.talkback)">Talkback</LibTip></h3></div>
        <ol class="ib-thread">
          <li v-for="{ event: e, acceptance } in thread" :key="e.id" class="ib-ev-row" :class="e.actor_kind">
            <span class="sm"><span class="bold">{{ eventLine(e, d.current.ops) }}</span> <span class="t2">{{ ago(e.at, now) }}</span></span>
            <p v-if="e.body" class="ib-body-text">{{ e.body }}</p>
            <InboxAcceptance v-if="acceptance" :acceptance="acceptance" :album-id="albumId" :phone="phone" />
          </li>
          <li v-for="a in otherAcceptances" :key="`acceptance-${a.id}`" class="ib-ev-row user">
            <span class="sm"><strong>{{ a.author }} accepted {{ a.ops.map(name).join(', ') }}</strong> <span class="t2">{{ ago(a.created_at, now) }}</span></span>
            <InboxAcceptance :acceptance="a" :album-id="albumId" :phone="phone" />
          </li>
        </ol>
        <form class="col g8 pad" aria-label="Reply" @submit.prevent="send">
          <label class="lbl-f">Reply<textarea v-model="reply" class="ib-text" rows="3" maxlength="4000"></textarea></label>
          <div class="line g12 wrap">
            <span v-if="!phone" class="grow"></span>
            <button v-if="hasPending" type="button" class="rocker" :class="{ flat: phone }" role="switch" :aria-checked="asking" @click="asking = !asking">
              <span class="rk" aria-hidden="true">ON<span class="rk-case"><span class="rk-paddle"></span></span>OFF</span><span class="rk-t"><span class="rk-name"><LibTip title="Ask for changes" :note="tip(inboxNotes.ask)">Ask for changes</LibTip></span></span>
            </button>
            <button type="submit" class="key sm ib-send" :class="{ 'ib-send-row': phone }" data-wide="Ask for changes" :disabled="inbox.busy || !reply.trim()"><span>{{ hasPending && asking ? 'Ask for changes' : 'Send comment' }}</span></button>
          </div>
        </form>
      </div>
    </div>
    <InboxBar v-if="d && !phone" @files="openFiles" />
  </section>
</template>

<style scoped>
.rs .pad { padding: 12px 14px; }
.rs .ib-body { scrollbar-width: thin; }
.rs .ib-head { display: flex; align-items: flex-start; gap: 14px; }
.rs .ib-head > .col { min-width: 0; }
.rs .ib-title { font-size: 19px; font-weight: 700; line-height: 1.25; overflow-wrap: anywhere; }
.rs .ib-conf { flex: none; align-items: center; gap: 2px; padding: 8px 10px; }
.rs .ib-conf .lcd-big { font-size: 24px; }
.rs .ib-conf .lcd-l { font-size: 11px; }
.rs .ib-why { margin: 0 14px 12px; }
.rs .ib-why .t2, .rs .ib-reason, .rs .ib-body-text { white-space: pre-wrap; overflow-wrap: anywhere; }
.rs .clamp2 { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.rs .ib-self { align-self: flex-start; }
.rs .ib-tabs { position: sticky; top: 0; z-index: 3; align-items: center; background: var(--win); }
.rs .ib-tabs .segk button { height: 36px; padding: 0 12px; font-size: 13px; }
.rs .ib-phone-tools { padding: 0 12px; border-bottom: 1px solid var(--line); }
.rs .ib-phone-tools button { min-height: 44px; }
.rs .ib-tools { min-height: 28px; }
.rs .ib-moved, .rs .ib-ev-row { border-bottom: 1px solid var(--line); }
.rs .ib-tapes, .rs .ib-thread, .rs .ib-ev-row { display: flex; flex-direction: column; }
.rs .ib-tapes { align-items: flex-start; gap: 8px; }
.rs .ib-tapes .sug { max-width: 100%; flex-wrap: wrap; padding-inline: calc(2% + 10px); }
.rs .ib-tapes .sug-v, .rs .ib-tapes .sug-s, .rs .meta dd { min-width: 0; white-space: normal; overflow-wrap: anywhere; }
.rs .sug.tape { cursor: default; }
.rs a.sug { text-decoration: none; }
.rs .ib-thread { margin: 0; padding: 0; list-style: none; }
.rs .ib-ev-row { gap: 4px; padding: 10px 14px; overflow-wrap: anywhere; }
.rs .ib-ev-row.user { box-shadow: inset 3px 0 0 var(--lamp); }
.rs .ib-ev-row.system { color: var(--text-2); }
.rs .key.ib-send { display: inline-grid; place-items: center; }
.rs .key.ib-send > span, .rs .key.ib-send::after { grid-area: 1 / 1; }
.rs .key.ib-send::after { content: attr(data-wide); visibility: hidden; }
.rs .key.ib-send-row { flex: 1 1 100%; }
@media (max-width: 719px) {
  .rs .pad { padding: 10px 12px; }
  .rs .ib-head { gap: 8px; }
  .rs .ib-tabs { padding: 8px; }
  .rs .ib-tabs .segk button { gap: 4px; height: 44px; padding: 0 4px; }
  .rs .ib-why .link { min-height: 30px; }
}
@media (max-width: 400px) {
  .rs .ib-detail .tb .stripes { display: none; }
}
</style>
