<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { copyText } from '../lib/copy'
import { useNow } from '../lib/clock'
import { ago } from '../lib/format'
import { useAgents } from '../stores/agents'
import { useInbox } from '../stores/inbox'
import PartCard from './PartCard.vue'

const agents = useAgents()
const inbox = useInbox()
const now = useNow()

const name = ref('')
const ops = ref<string[]>([])
const asking = ref<number | null>(null)
const copied = ref('')
const tokenEl = ref<HTMLElement | null>(null)
const promptEl = ref<HTMLElement | null>(null)

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
async function copy(what: 'token' | 'prompt') {
  const text = what === 'token' ? agents.created?.token : agents.setup?.prompt
  if (!text) return
  const ok = await copyText(text, what === 'token' ? tokenEl.value : promptEl.value)
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
          <span class="bold">Secret for {{ agents.created.connection.name }}</span>
          <span class="sub">Shelf shows it once. Give it to the agent as SHELF_AGENT_TOKEN, apart from the prompt, and never in a URL or chat log.</span>
          <code ref="tokenEl" class="mono tok">{{ agents.created.token }}</code>
          <div class="line g8 wrap">
            <button type="button" class="key sm sun" @click="copy('token')">Copy secret</button>
            <button type="button" class="key sm" @click="agents.forget()">I stored it</button>
          </div>
        </div>
      </div>

      <ul v-if="agents.connections.length" class="conns" aria-label="Connections">
        <li v-for="c in active" :key="c.id" class="conn">
          <div class="col g4 grow">
            <span class="bold">{{ c.name }}</span>
            <span class="t2 sm">Added by {{ c.created_by }} {{ ago(c.created_at, now) }} · {{ c.last_used_at ? `last used ${ago(c.last_used_at, now)}` : 'not used yet' }}</span>
            <span class="t2 xs mono">{{ c.operations.join(' ') }}</span>
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
          <label v-for="o in agents.setup.operations" :key="o.name" class="ib-check sm"><input type="checkbox" :checked="ops.includes(o.name)" @change="toggleOp(o.name)" /><span><span class="mono">{{ o.name }}</span> · {{ o.summary }}</span></label>
        </fieldset>
        <button type="submit" class="key sm self" :disabled="agents.busy || !name.trim()">Connect an agent</button>
      </form>
      <p v-else class="t2 sm">Without Shelf sign-in, connections come from the shell: <code class="mono">shelf manage agents add &lt;name&gt;</code>. Listing and revoking work here.</p>

      <details v-if="agents.setup" class="setup">
        <summary class="bold">Setup prompt</summary>
        <div class="col g8">
          <p class="t2 sm">The prompt holds no secret. The agent reads SHELF_AGENT_TOKEN from its environment. The token reaches tags, lyrics, file names, history and authors.</p>
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
.rs .setup summary { cursor: pointer; min-height: 32px; }
.rs .prompt { max-height: 320px; margin: 0; padding: 10px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; background: var(--field); border: 1px solid var(--ink); border-radius: 5px; color: var(--text); }
</style>
