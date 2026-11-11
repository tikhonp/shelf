<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import type { Connection } from '../api/agents'
import { useNow } from '../lib/clock'
import { ago, plural, shortDay } from '../lib/format'
import { usePhone } from '../lib/viewport'
import { useAgents } from '../stores/agents'
import { useInbox } from '../stores/inbox'
import AgentSecret from './AgentSecret.vue'
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
// asking is the connection whose Revoke or New secret waits for a second press.
const asking = ref<{ id: number; what: 'revoke' | 'renew' } | null>(null)
// choosing shows the permission rockers. Every operation is on unless the user opens them.
const choosing = ref(false)

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
// Only a new connection's prompt locks Connect. A new secret shows in its own connection's unit.
const fresh = computed(() => (agents.created && !agents.created.renewed ? agents.created : null))
const renewed = (c: Connection) => (agents.created?.renewed && agents.created.connection.id === c.id ? agents.created : null)
const locked = computed(() => !!fresh.value)
const allowed = computed(() => {
  const n = allOps().length
  return ops.value.length === n ? 'Everything' : ops.value.length === 0 ? 'Nothing' : `${ops.value.length} of ${n}`
})

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
const asks = (c: Connection, what: 'revoke' | 'renew') => asking.value?.id === c.id && asking.value.what === what
const jack = (c: Connection) => (c.revoked_at ? 'out' : asks(c, 'revoke') ? 'asking' : 'in')

async function create() {
  await agents.create(name.value.trim(), ops.value)
}
async function revoke(id: number) {
  if (await agents.revoke(id)) asking.value = null
}
async function renew(id: number) {
  if (await agents.renew(id)) asking.value = null
}
function close() {
  agents.forget()
  name.value = ''
  ops.value = allOps()
  choosing.value = false
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
          <button v-else type="button" class="jackbtn" :aria-label="`Revoke ${c.name}`" :aria-expanded="asks(c, 'revoke')" @click="asking = { id: c.id, what: 'revoke' }"><PatchJack /><span class="gear-name" aria-hidden="true">Revoke</span></button>
          <span v-if="c.revoked_at" class="sub">Unplugged. Its proposals stay in the Inbox.</span>
          <template v-else-if="asks(c, 'revoke')">
            <span class="sub">Revoke {{ c.name }}? Its token stops working at once.</span>
            <button type="button" class="key sm" @click="asking = null">Cancel</button>
            <button type="button" class="key sm sun" :disabled="agents.busy" @click="revoke(c.id)">Revoke</button>
          </template>
          <template v-else-if="asks(c, 'renew')">
            <span class="sub">Give {{ c.name }} a new secret? The old one stops working at once.</span>
            <button type="button" class="key sm" @click="asking = null">Cancel</button>
            <button type="button" class="key sm sun" :disabled="agents.busy" @click="renew(c.id)">New secret</button>
          </template>
          <button v-else-if="agents.canCreate && !renewed(c)" type="button" class="key sm" @click="asking = { id: c.id, what: 'renew' }"><LibTip title="New secret" note="Shelf keeps only a hash of the secret, so it can't show the old one. A new secret keeps the connection's permissions, proposals and memory.">New secret</LibTip></button>
        </div>
        <AgentSecret v-if="renewed(c)" :key="renewed(c)!.token" :created="renewed(c)!" @close="agents.forget()" />
      </div>
    </PartCard>

    <PartCard name="Connect" lamp="off" note="Shelf shows the prompt once" :lines="[]" class="ag-c" last>
      <form v-if="agents.canCreate" class="col g14 ag-form" aria-label="Connect an agent" @submit.prevent="create">
        <label class="lbl-f">Name<input v-model="name" class="input" maxlength="64" placeholder="Claude at home" autocomplete="off" required :readonly="locked" :aria-readonly="locked" /></label>
        <fieldset v-if="agents.setup?.operations.length" class="ops">
          <legend class="lbl-f">Allowed</legend>
          <div class="line g10">
            <span class="t2">{{ allowed }}</span>
            <button type="button" class="key sm" :aria-expanded="choosing" aria-controls="ag-ops" @click="choosing = !choosing">{{ choosing ? 'Hide' : 'Choose' }}</button>
          </div>
          <div v-if="choosing" id="ag-ops" class="lib-bank op-grid">
            <button v-for="o in agents.setup.operations" :key="o.name" type="button" class="rocker" role="switch" :aria-checked="ops.includes(o.name)" :aria-disabled="locked ? 'true' : undefined" @click="toggleOp(o.name)">
              <span class="rk" aria-hidden="true">ON<span class="rk-case"><span class="rk-paddle"></span></span>OFF</span><span class="rk-t"><span class="rk-name">{{ operationWords[o.name]?.name ?? o.name }}</span><span class="rk-state">{{ operationWords[o.name]?.summary ?? o.summary }}</span></span>
            </button>
          </div>
        </fieldset>
        <div><button type="submit" class="key" :class="{ sun: !locked, block: phone }" :disabled="agents.busy || locked || !name.trim()"><LibTip title="Connect" note="Makes a connection with the permissions that are on. No agent can accept, save or delete, whatever it is allowed.">Connect</LibTip></button></div>

        <AgentSecret v-if="fresh" :key="fresh.token" :created="fresh" @close="close" />
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
.rs .smod .ag-gear { justify-content: space-between; gap: 10px; }
.rs .smod .msgbtn, .rs .smod .jackbtn { min-height: 54px; }
.rs .smod .fdbtn { min-height: 46px; }
.rs .smod .op-grid { grid-template-columns: 1fr; gap: 2px; }
.rs .smod .key.sm { min-height: 46px; }
</style>
