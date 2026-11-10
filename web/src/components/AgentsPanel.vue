<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import type { Connection } from '../api/agents'
import { copyText } from '../lib/copy'
import { useNow } from '../lib/clock'
import { ago, plural, shortDay } from '../lib/format'
import { usePhone } from '../lib/viewport'
import { useAgents } from '../stores/agents'
import { useInbox } from '../stores/inbox'
import LibTip from './LibTip.vue'
import PartCard from './PartCard.vue'
import PatchJack from './PatchJack.vue'

const operationWords: Record<string, { name: string; summary: string }> = {
  'library.read': { name: 'Read the library', summary: 'Albums, tags, local suggestions' },
  'library.lookup': { name: 'Look up', summary: 'MusicBrainz, sources, LRCLIB' },
  'history.read': { name: 'History', summary: 'Changes and who made them' },
  'covers.upload': { name: 'Upload covers', summary: 'Images for proposals' },
  'proposals.write': { name: 'Propose', summary: 'Create, revise, comment, withdraw' },
  'memory.write': { name: 'Memory', summary: 'Notes kept between runs' },
}
const agents = useAgents()
const inbox = useInbox()
const now = useNow()
const phone = usePhone()

const name = ref('')
const ops = ref<string[]>([])
const asking = ref<number | null>(null)
// copied is whether the user copied the new prompt or secret, so Done can ask before the secret goes.
const copied = ref(false)
const copyNote = ref('')
const closing = ref(false)
const tokenEl = ref<HTMLElement | null>(null)
const promptEl = ref<HTMLElement | null>(null)

const allOps = () => agents.setup?.operations.map((o) => o.name) ?? []
onMounted(() => { if (inbox.enabled) void agents.load() })
onBeforeUnmount(() => agents.forget())
watch(() => inbox.enabled, (on) => { if (on) void agents.load() })
// Every operation starts on, as the CLI's default does.
watch(() => agents.setup?.operations, (list) => { if (list && ops.value.length === 0) ops.value = allOps() }, { immediate: true })

const active = computed(() => agents.connections.filter((c) => !c.revoked_at))
// Active connections come first. One list keeps a unit's element when it is revoked, so its plug slides out.
const units = computed(() => [...active.value, ...agents.connections.filter((c) => c.revoked_at)])
const pending = computed(() => agents.connections.reduce((n, c) => n + (c.proposals ?? 0), 0))
const locked = computed(() => !!agents.created)

const up = (s: string) => s.toUpperCase()
function facts(c: Connection): string[] {
  if (c.revoked_at) return [`REVOKED ${up(ago(c.revoked_at, now.value))}`, 'TOKEN REFUSED']
  const run = c.last_run_at ?? c.last_used_at
  const work = [plural(c.proposals ?? 0, 'proposal'), ...(c.rules ? [plural(c.rules, 'rule')] : [])].join(' · ')
  return [run ? `LAST RUN ${up(ago(run, now.value))}` : 'NOT USED YET', up(work)]
}
const reasonWord = (r: string) => (r === 'account_deleted' ? 'Its creator was deleted' : r === 'cli' ? 'Revoked from the CLI' : 'Revoked')
function note(c: Connection): string {
  if (c.revoked_at) return c.revoked_by ? `Revoked by ${c.revoked_by}` : reasonWord(c.revoke_reason)
  return phone.value ? `Added by ${c.created_by}` : `Added by ${c.created_by} on ${shortDay(c.created_at)}`
}
const jack = (c: Connection) => (c.revoked_at ? 'out' : asking.value === c.id ? 'asking' : 'in')

async function create() {
  copied.value = false
  copyNote.value = ''
  closing.value = false
  await agents.create(name.value.trim(), ops.value)
}
async function revoke(id: number) {
  if (await agents.revoke(id)) asking.value = null
}
async function copy(what: 'token' | 'prompt') {
  const text = what === 'token' ? agents.created?.token : agents.created?.prompt
  if (!text) return
  const ok = await copyText(text, what === 'token' ? tokenEl.value : promptEl.value)
  copied.value = true
  closing.value = false
  copyNote.value = ok ? (what === 'token' ? 'Secret copied.' : 'Prompt copied.') : 'Copy it from the selected text.'
}
function done() {
  if (!copied.value) {
    closing.value = true
    return
  }
  close()
}
function close() {
  agents.forget()
  name.value = ''
  ops.value = allOps()
  copied.value = false
  copyNote.value = ''
  closing.value = false
}
function toggleOp(n: string) {
  if (locked.value) return
  ops.value = ops.value.includes(n) ? ops.value.filter((x) => x !== n) : [...ops.value, n]
}
</script>

<template>
  <template v-if="inbox.enabled">
    <PartCard name="Agents" :lamp="active.length ? 'ok' : 'off'" note="A person accepts each change" :lines="[`${active.length} CONNECTED`, `${plural(pending, 'proposal')} pending`.toUpperCase()]">
      <div class="ag-gear">
        <RouterLink class="msgbtn" to="/inbox" :data-lit="pending > 0 ? '' : undefined" :aria-label="pending > 0 ? 'Open the Inbox, proposals wait' : 'Open the Inbox'">
          <span class="msg-cap" aria-hidden="true"><span class="msg-lens"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 13h5l1.5 3h5L16 13h5" /><path d="M5 5h14l2 8v6H3v-6z" /></svg></span></span><span class="gear-name" aria-hidden="true">{{ phone ? 'Inbox' : 'Open the Inbox' }}</span>
        </RouterLink>
        <RouterLink class="key fdbtn" to="/inbox/memory"><span class="fd" aria-hidden="true"><span class="fd-disk"></span><span class="fd-face"></span></span>Memory</RouterLink>
      </div>
      <p v-if="agents.error" class="bad-text sm ag-full" role="alert">{{ agents.error }}</p>
    </PartCard>

    <PartCard v-for="c in units" :key="c.id" class="ag-c" :class="{ gone: c.revoked_at }" :name="c.name" :lamp="c.revoked_at ? 'off' : 'ok'" :note="note(c)" :lines="facts(c)">
      <div class="col g10 ag-side">
        <div v-if="!c.revoked_at" class="stks" aria-label="Allowed"><span v-for="op in c.operations" :key="op" class="stk">{{ operationWords[op]?.name ?? op }}</span></div>
        <div class="line g10 wrap" :data-jack="jack(c)">
          <span v-if="c.revoked_at" class="jackbtn idle" aria-hidden="true"><PatchJack /></span>
          <button v-else type="button" class="jackbtn" :aria-label="`Revoke ${c.name}`" :aria-expanded="asking === c.id" @click="asking = c.id"><PatchJack /><span class="gear-name" aria-hidden="true">Revoke</span></button>
          <span v-if="c.revoked_at" class="sub">Unplugged. Its proposals stay in the Inbox.</span>
          <template v-else-if="asking === c.id">
            <span class="sub">Revoke {{ c.name }}? Its token stops working at once.</span>
            <button type="button" class="key sm" @click="asking = null">Cancel</button>
            <button type="button" class="key sm sun" :disabled="agents.busy" @click="revoke(c.id)">Revoke</button>
          </template>
        </div>
      </div>
    </PartCard>

    <PartCard name="Connect" lamp="off" note="Shelf shows the prompt once" :lines="[]" class="ag-c" last>
      <form v-if="agents.canCreate" class="col g14 ag-form" aria-label="Connect an agent" @submit.prevent="create">
        <label class="lbl-f">Name<input v-model="name" class="input" maxlength="64" placeholder="Claude at home" autocomplete="off" required :readonly="locked" :aria-readonly="locked" /></label>
        <fieldset v-if="agents.setup?.operations.length" class="ops">
          <legend class="lbl-f">Allowed</legend>
          <div class="lib-bank op-grid">
            <button v-for="o in agents.setup.operations" :key="o.name" type="button" class="rocker" role="switch" :aria-checked="ops.includes(o.name)" :aria-disabled="locked ? 'true' : undefined" @click="toggleOp(o.name)">
              <span class="rk" aria-hidden="true">ON<span class="rk-case"><span class="rk-paddle"></span></span>OFF</span><span class="rk-t"><span class="rk-name">{{ operationWords[o.name]?.name ?? o.name }}</span><span class="rk-state">{{ operationWords[o.name]?.summary ?? o.summary }}</span></span>
            </button>
          </div>
        </fieldset>
        <div><button type="submit" class="key" :class="{ sun: !locked, block: phone }" :disabled="agents.busy || locked || !name.trim()"><LibTip title="Connect" note="Makes a connection with the permissions that are on. No agent can accept, save or delete, whatever it is allowed.">Connect</LibTip></button></div>

        <div v-if="agents.created" class="note secret" role="region" aria-label="New connection prompt">
          <span class="bold">Prompt for {{ agents.created.connection.name }}</span>
          <span class="sub">Shelf shows it once. The prompt holds the connection's secret, so paste it only into the agent. The secret reaches tags, lyrics, file names, history and authors.</span>
          <pre ref="promptEl" class="mono prompt">{{ agents.created.prompt }}</pre>
          <span class="sub">The secret alone, for SHELF_AGENT_TOKEN</span>
          <code ref="tokenEl" class="mono tok">{{ agents.created.token }}</code>
          <div class="line g10 wrap">
            <button type="button" class="key sm sun" @click="copy('prompt')">Copy prompt</button>
            <button type="button" class="key sm" @click="copy('token')">Copy secret</button>
            <span v-if="copyNote" class="sub" role="status">{{ copyNote }}</span>
            <span class="grow"></span>
            <template v-if="closing">
              <span class="sub" role="alert">Nothing copied yet, and Shelf shows it only once.</span>
              <button type="button" class="key sm" @click="close">Close anyway</button>
            </template>
            <button v-else type="button" class="key sm" @click="done">Done</button>
          </div>
        </div>
      </form>
      <p v-else class="t2 sm ag-form">Without Shelf sign-in, connections come from the shell: <code class="mono">shelf manage agents add &lt;name&gt;</code>. Listing and revoking work here.</p>
    </PartCard>
  </template>
</template>

<style scoped>
.rs .ag-gear { display: flex; align-items: center; gap: 18px; }
.rs .ag-full { flex: 1 1 100%; }
.rs .ag-c :deep(.unit-body) { align-items: flex-start; }
.rs .ag-c :deep(.plate h2) { overflow-wrap: anywhere; }
.rs .ag-c.gone { opacity: .8; }
.rs .ag-side { flex: 1 1 260px; min-width: 0; }
.rs .ag-form { flex: 1 1 640px; min-width: 0; }
.rs .stks { display: flex; flex-wrap: wrap; gap: 6px; }
.rs .jackbtn.idle { cursor: default; }
.rs .ops { display: flex; flex-direction: column; gap: 8px; margin: 0; padding: 0; border: 0; }
.rs .ops legend { padding: 0 0 8px; }
.rs .op-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; }
.rs .op-grid .rk-name, .rs .op-grid .rk-state { white-space: normal; }
.rs .secret { flex-direction: column; align-items: stretch; gap: 10px; border-color: var(--sun); }
.rs .tok { display: block; padding: 8px 10px; overflow-wrap: anywhere; background: var(--field); border: 1px solid var(--ink); border-radius: 5px; color: var(--text); user-select: all; }
.rs .prompt { max-height: 230px; margin: 0; padding: 10px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; background: var(--field); border: 1px solid var(--ink); border-radius: 5px; color: var(--text); }
.rs .smod .ag-gear { justify-content: space-between; gap: 10px; }
.rs .smod .msgbtn, .rs .smod .jackbtn { min-height: 54px; }
.rs .smod .fdbtn { min-height: 46px; }
.rs .smod .op-grid { grid-template-columns: 1fr; gap: 2px; }
.rs .smod .key.sm { min-height: 46px; }
</style>
