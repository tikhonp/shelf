<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'
import InboxBar from '../components/InboxBar.vue'
import InboxDetail from '../components/InboxDetail.vue'
import InboxList from '../components/InboxList.vue'
import LibSplit from '../components/LibSplit.vue'
import { vVars } from '../lib/vars'
import { usePhone, useStacked } from '../lib/viewport'
import { useAgents } from '../stores/agents'
import { useHelp } from '../stores/help'
import { filtersFromQuery, filtersToQuery, useInbox } from '../stores/inbox'

const route = useRoute()
const router = useRouter()
const phone = usePhone()
const stacked = useStacked()
const list = ref<InstanceType<typeof InboxList>>()
const proposal = ref<InstanceType<typeof InboxDetail>>()
const inbox = useInbox()
const agents = useAgents()
const help = useHelp()

function read(name: string, fallback: number): number {
  try {
    return Number(localStorage.getItem(name)) || fallback
  } catch {
    return fallback
  }
}
function keep(name: string, value: number) {
  try {
    localStorage.setItem(name, String(value))
  } catch {
    // The width then lasts only this visit.
  }
}
const clamp = (v: number) => Math.min(640, Math.max(300, v))
const listW = ref(clamp(read('inbox.list', 420)))
function moveList(d: number) {
  listW.value = clamp(listW.value + d)
  keep('inbox.list', listW.value)
}
const widths = computed(() => ({ '--list-w': `${listW.value}px` }))

const id = computed(() => (route.params.id ? Number(route.params.id) : undefined))
const close = () => void router.push({ path: '/inbox', query: filtersToQuery(inbox.filters) })

const leaveAsk = ref(false)
const leaveDialog = ref<HTMLElement>()
const keepEditingBtn = ref<HTMLButtonElement>()
let leaveResolve: ((allow: boolean) => void) | null = null
let leaveReturnFocus: HTMLElement | null = null

const focusableSelector = 'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'

function onLeaveKey(e: KeyboardEvent) {
  if (!leaveAsk.value) return
  const modals = [...document.querySelectorAll<HTMLElement>('[aria-modal="true"]')]
  if (leaveDialog.value && modals.at(-1) !== leaveDialog.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    e.stopImmediatePropagation()
    resolveLeave(false)
    return
  }
  if (e.key === 'Tab') {
    const nodes = [...(leaveDialog.value?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])]
    if (nodes.length === 0) {
      e.preventDefault()
      e.stopImmediatePropagation()
      keepEditingBtn.value?.focus()
      return
    }
    const first = nodes[0]!
    const last = nodes[nodes.length - 1]!
    const active = document.activeElement
    if (e.shiftKey) {
      if (active === first || !leaveDialog.value?.contains(active)) {
        e.preventDefault()
        e.stopImmediatePropagation()
        last.focus()
      }
    } else if (active === last || !leaveDialog.value?.contains(active)) {
      e.preventDefault()
      e.stopImmediatePropagation()
      first.focus()
    }
  }
}

watch(leaveAsk, (open) => {
  if (open) document.addEventListener('keydown', onLeaveKey, true)
  else document.removeEventListener('keydown', onLeaveKey, true)
})

const leaveMessage = computed(() => {
  const items: string[] = []
  if (inbox.hasValueDrafts) items.push('unsaved value edits')
  if (inbox.hasReplyDraft) items.push('an unsent Talkback reply')
  if (inbox.hasDeclineDraft) items.push('an unsent Decline reason')
  if (items.length === 0) return 'Leaving this proposal discards your unsaved changes.'
  const list = items.length === 1 ? items[0] : items.length === 2 ? `${items[0]} and ${items[1]}` : `${items.slice(0, -1).join(', ')}, and ${items.at(-1)}`
  return `You have ${list}. Leaving this proposal discards ${items.length === 1 && !inbox.hasValueDrafts ? 'it' : 'them'}.`
})

function resolveLeave(allow: boolean) {
  const done = leaveResolve
  const restore = !allow ? leaveReturnFocus : null
  leaveResolve = null
  leaveReturnFocus = null
  leaveAsk.value = false
  done?.(allow)
  if (restore?.isConnected) void nextTick(() => restore.focus())
}

function confirmDiscard(): Promise<boolean> {
  if (!inbox.hasUnsaved) return Promise.resolve(true)
  if (leaveResolve) resolveLeave(false)
  leaveReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  leaveAsk.value = true
  void nextTick(() => keepEditingBtn.value?.focus())
  return new Promise<boolean>((resolve) => {
    leaveResolve = resolve
  })
}

onBeforeRouteUpdate((to, from) => {
  const fromId = String(from.params.id ?? '')
  const toId = String(to.params.id ?? '')
  if (!fromId || toId === fromId) return true
  return confirmDiscard()
})

onBeforeRouteLeave(() => {
  if (inbox.enabled === false || !id.value) return true
  return confirmDiscard()
})

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (!inbox.hasUnsaved) return
  e.preventDefault()
  e.returnValue = ''
}

// With the feature off the inbox doesn't exist, so its addresses lead back to the library.
watch(
  () => inbox.enabled,
  (on) => {
    if (on === false) void router.replace('/library')
  },
  { immediate: true },
)

async function retryDetect() {
  await inbox.detect()
  if (inbox.enabled) {
    void agents.load()
    void inbox.loadList()
    if (id.value) void inbox.open(id.value)
  }
}

const pendingFocusId = ref<number | null>(null)
function tryRestoreFocus() {
  if (pendingFocusId.value === null || id.value) return
  if (list.value?.focusRow(pendingFocusId.value)) {
    pendingFocusId.value = null
  }
}

onMounted(async () => {
  window.addEventListener('beforeunload', onBeforeUnload)
  if (inbox.enabled === null) await inbox.detect()
  if (inbox.enabled) void agents.load()
})

watch(
  () => route.query,
  (q) => {
    if (inbox.enabled === false) return
    if (route.name === 'inbox' || route.name === 'proposal') inbox.setFilters(filtersFromQuery(q))
  },
  { immediate: true },
)

watch(
  id,
  async (next, prev) => {
    help.note = null
    if (inbox.enabled === false) return
    if (next) {
      pendingFocusId.value = null
      void inbox.open(next)
      if (stacked.value) {
        await nextTick()
        proposal.value?.focusHeading(!phone.value)
      }
    } else {
      inbox.close()
      if (prev && stacked.value) {
        pendingFocusId.value = prev
        await nextTick()
        tryRestoreFocus()
      }
    }
  },
  { immediate: true },
)

watch(
  () => inbox.proposals,
  () => {
    if (pendingFocusId.value !== null) void nextTick(tryRestoreFocus)
  },
)
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', onBeforeUnload)
  document.removeEventListener('keydown', onLeaveKey, true)
  if (leaveResolve) resolveLeave(false)
  help.note = null
  inbox.close()
})
</script>

<template>
  <div v-if="inbox.enabled === null && inbox.detectError" :class="phone ? 'stack' : 'page'">
    <section class="win ib-detect-err" aria-labelledby="ib-err-h">
      <div class="tb">
        <RouterLink v-if="phone" class="wbtn" to="/library" aria-label="Back to the library" title="Library"><Icon name="back" /></RouterLink>
        <h1 id="ib-err-h">Inbox</h1>
        <span class="stripes"></span>
      </div>
      <div class="pad col g8" role="alert">
        <p class="bad-text">{{ inbox.detectError }}</p>
        <div class="line g8">
          <button type="button" class="key sm" :disabled="inbox.detecting" @click="retryDetect">Retry</button>
        </div>
      </div>
    </section>
  </div>
  <div v-else-if="phone" class="stack">
    <InboxDetail v-if="id" :key="id" ref="proposal" phone @close="close" />
    <div v-if="id && help.note" class="win slot tall" role="note">
      <div class="lib-note">
        <span class="tip-head"><span class="grow bold">{{ help.note.title }}</span><button type="button" class="wbtn" aria-label="Close the note" @click="help.note = null"><Icon name="close" /></button></span>
        <p class="t2">{{ help.note.text }}</p>
      </div>
    </div>
    <InboxBar v-if="id" :key="id" phone @files="proposal?.openFiles($event)" />
    <InboxList v-else ref="list" />
  </div>
  <main v-else v-vars="widths" class="lib-main ib-page">
    <InboxList ref="list" class="ib-list" :class="{ wide: !id }" :selected="id" />
    <LibSplit v-if="id" label="proposal list" @move="moveList" />
    <InboxDetail v-if="id" :key="id" ref="proposal" class="ib-main" @close="close" />
  </main>
  <div v-if="leaveAsk" class="scrim">
    <section ref="leaveDialog" class="win dlg" role="alertdialog" aria-modal="true" aria-labelledby="ib-leave-t" aria-describedby="ib-leave-d">
      <div class="tb"><span class="led warn"></span><h2 id="ib-leave-t">Discard unsaved changes?</h2><span class="stripes"></span></div>
      <div id="ib-leave-d" class="ask-body"><p>{{ leaveMessage }}</p></div>
      <div class="ask-keys">
        <button ref="keepEditingBtn" type="button" class="key" @click="resolveLeave(false)">Keep editing</button>
        <button type="button" class="key sun" @click="resolveLeave(true)">Discard</button>
      </div>
    </section>
  </div>
</template>

<style scoped>
/* As in the Library, each window scrolls by itself, so the menubar and the divider stay put. */
.rs .ib-list { flex: 0 0 var(--list-w); }
.rs .ib-list.wide, .rs .ib-main { flex: 1 1 0; }
.rs .page > .ib-detect-err { flex: 1; max-width: 640px; }
.rs .ib-detect-err .pad { padding: 14px; }
.rs .scrim { position: fixed; inset: 0; z-index: 50; display: flex; align-items: center; justify-content: center; padding: 20px; background: rgba(6, 8, 12, .6); }
.rs .dlg { width: 100%; max-width: 480px; }
.rs .ask-body { display: flex; flex-direction: column; gap: 10px; padding: 18px 18px 14px; }
.rs .ask-keys { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 12px; padding: 0 18px 18px; }
@media (max-width: 719px) {
  .rs .scrim { padding: 8px; }
  .rs .ask-body { padding: 12px 12px 10px; }
  .rs .ask-keys { padding: 0 12px 12px; }
}
</style>
