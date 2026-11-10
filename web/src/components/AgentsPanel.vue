<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { copyText } from '../lib/copy'
import { useNow } from '../lib/clock'
import { ago } from '../lib/format'
import { useAgents } from '../stores/agents'
import { useInbox } from '../stores/inbox'
import PartCard from './PartCard.vue'

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

const name = ref('')
const ops = ref<string[]>([])
const asking = ref<number | null>(null)
const copied = ref('')
const tokenEl = ref<HTMLElement | null>(null)
const promptEl = ref<HTMLElement | null>(null)
const newPromptEl = ref<HTMLElement | null>(null)

onMounted(() => { if (inbox.enabled) void agents.load() })
onBeforeUnmount(() => agents.forget())
watch(() => inbox.enabled, (on) => { if (on) void agents.load() })
// Every operation starts on, as the CLI's default does.
watch(() => agents.setup?.operations, (list) => { if (list && ops.value.length === 0) ops.value = list.map((o) => o.name) }, { immediate: true })

const active = computed(() => agents.connections.filter((c) => !c.revoked_at))
const revoked = computed(() => agents.connections.filter((c) => c.revoked_at))
const reasonWord = (r: string) => (r === 'account_deleted' ? 'its creator was deleted' : r === 'cli' ? 'from the CLI' : r === 'user' ? 'by a user' : '')

async function create() {
  if (await agents.create(name.value.trim(), ops.value)) name.value = ''
}
async function revoke(id: number) {
  if (await agents.revoke(id)) asking.value = null
}
async function copy(what: 'token' | 'prompt' | 'new') {
  const text = what === 'token' ? agents.created?.token : what === 'new' ? agents.created?.prompt : agents.setup?.prompt
  if (!text) return
  const ok = await copyText(text, what === 'token' ? tokenEl.value : what === 'new' ? newPromptEl.value : promptEl.value)
  copied.value = ok ? (what === 'token' ? 'Secret copied.' : 'Prompt copied.') : 'Copy it from the selected text.'
}
const toggleOp = (n: string) => (ops.value = ops.value.includes(n) ? ops.value.filter((x) => x !== n) : [...ops.value, n])
</script>

<template>
  <PartCard v-if="inbox.enabled" name="Agents" :lamp="active.length ? 'ok' : 'off'" :note="`${active.length} connected`" :lines="[]">
    <div class="col g12 ag">
      <p class="t2 sm">An agent reads the library and proposes tag and cover changes. Nothing changes until a user accepts it in the <RouterLink to="/inbox">Inbox</RouterLink>. Its memory is in <RouterLink to="/inbox/memory">Memory</RouterLink>.</p>
      <p v-if="agents.error" class="bad-text sm" role="alert">{{ agents.error }}</p>
      <p v-if="copied" class="t2 sm" role="status">{{ copied }}</p>

      <div v-if="agents.created" class="note secret" role="region" aria-label="New connection secret">
        <div class="col g8 grow">
          <span class="bold">Prompt for {{ agents.created.connection.name }}</span>
          <span class="sub">Shelf shows it once. The prompt holds the connection's secret, so paste it only into the agent. The secret reaches tags, lyrics, file names, history and authors.</span>
          <pre ref="newPromptEl" class="mono prompt">{{ agents.created.prompt }}</pre>
          <span class="sub">The secret alone, for SHELF_AGENT_TOKEN</span>
          <code ref="tokenEl" class="mono tok">{{ agents.created.token }}</code>
          <div class="line g8 wrap">
            <button type="button" class="key sm sun" @click="copy('new')">Copy prompt</button>
            <button type="button" class="key sm" @click="copy('token')">Copy secret</button>
            <button type="button" class="key sm" @click="agents.forget()">I stored it</button>
          </div>
        </div>
      </div>

      <ul v-if="agents.connections.length" class="conns" aria-label="Connections">
        <li v-for="c in active" :key="c.id" class="conn">
          <div class="col g4 grow">
            <span class="bold">{{ c.name }}</span>
            <span class="t2 sm">Added by {{ c.created_by }} {{ ago(c.created_at, now) }} · {{ c.last_used_at ? `last used ${ago(c.last_used_at, now)}` : 'not used yet' }}</span>
            <span class="stks"><span v-for="op in c.operations" :key="op" class="stk">{{ operationWords[op]?.name ?? op }}</span></span>
          </div>
          <template v-if="asking === c.id">
            <button type="button" class="key sm" @click="asking = null">Cancel</button>
            <button type="button" class="key sm sun" :disabled="agents.busy" @click="revoke(c.id)">Revoke</button>
          </template>
          <button v-else type="button" class="key sm" @click="asking = c.id">Revoke</button>
        </li>
        <li v-for="c in revoked" :key="c.id" class="conn off">
          <div class="col g4 grow">
            <span class="bold">{{ c.name }}</span>
            <span class="t2 sm">Revoked {{ ago(c.revoked_at!, now) }}<template v-if="reasonWord(c.revoke_reason)">, {{ reasonWord(c.revoke_reason) }}</template></span>
          </div>
        </li>
      </ul>
      <p v-else class="t2 sm">No agent is connected.</p>

      <form v-if="agents.canCreate" class="col g8" aria-label="Connect an agent" @submit.prevent="create">
        <label class="lbl-f">Name<input v-model="name" class="lib-in" maxlength="64" placeholder="Claude at home" required /></label>
        <fieldset v-if="agents.setup?.operations.length" class="ops">
          <legend class="lbl-f">Allowed</legend>
          <div class="op-grid">
            <button v-for="o in agents.setup.operations" :key="o.name" type="button" class="rocker flat" role="switch" :aria-checked="ops.includes(o.name)" @click="toggleOp(o.name)">
              <span class="rk" aria-hidden="true"><span class="rk-case"><span class="rk-paddle"></span></span></span><span class="rk-t"><span class="rk-name">{{ operationWords[o.name]?.name ?? o.name }}</span><span class="rk-state">{{ operationWords[o.name]?.summary ?? o.summary }}</span></span>
            </button>
          </div>
        </fieldset>
        <div class="line g8 wrap"><button type="submit" class="key sm sun" :disabled="agents.busy || !name.trim()">Connect</button><span class="t2 sm">No agent can accept, save or delete.</span></div>
      </form>
      <p v-else class="t2 sm">Without Shelf sign-in, connections come from the shell: <code class="mono">shelf manage agents add &lt;name&gt;</code>. Listing and revoking work here.</p>

      <details v-if="agents.setup" class="setup">
        <summary class="bold">Setup prompt</summary>
        <div class="col g8">
          <p class="t2 sm">This copy holds no secret. An agent that uses it reads SHELF_AGENT_TOKEN from its environment. The token reaches tags, lyrics, file names, history and authors.</p>
          <p class="sm">API <code class="mono">{{ agents.setup.api_base }}</code></p>
          <pre ref="promptEl" class="mono prompt">{{ agents.setup.prompt }}</pre>
          <button type="button" class="key sm self" @click="copy('prompt')">Copy prompt</button>
        </div>
      </details>
    </div>
  </PartCard>
</template>

<style scoped>
.rs .ag { flex: 1 1 100%; min-width: 0; }
.rs .self { align-self: flex-start; }
.rs .conns { display: flex; flex-direction: column; margin: 0; padding: 0; list-style: none; border-top: 1px solid var(--line); }
.rs .conn { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 10px 0; border-bottom: 1px solid var(--line); }
.rs .conn.off { opacity: .75; }
.rs .secret { border-color: var(--sun); }
.rs .tok { display: block; padding: 8px 10px; overflow-wrap: anywhere; background: var(--field); border: 1px solid var(--ink); border-radius: 5px; color: var(--text); user-select: all; }
.rs .ops { display: flex; flex-direction: column; gap: 6px; margin: 0; padding: 0; border: 0; }
.rs .op-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px 12px; }
.rs .op-grid .rk-name, .rs .op-grid .rk-state { white-space: normal; }
@media (max-width: 600px) { .rs .op-grid { grid-template-columns: 1fr; } }
.rs .setup summary { cursor: pointer; min-height: 32px; }
.rs .prompt { max-height: 320px; margin: 0; padding: 10px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; background: var(--field); border: 1px solid var(--ink); border-radius: 5px; color: var(--text); }
</style>
