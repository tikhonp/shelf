<script setup lang="ts">
import { ref } from 'vue'
import type { Created } from '../stores/agents'
import { copyText } from '../lib/copy'

// The one-time prompt and secret of a new connection, or of a connection that got a new secret.
const props = defineProps<{ created: Created }>()
const emit = defineEmits<{ close: [] }>()

// copied is whether the user copied the prompt or secret, so Done can ask before the secret goes.
const copied = ref(false)
const copyNote = ref('')
const closing = ref(false)
const tokenEl = ref<HTMLElement | null>(null)
const promptEl = ref<HTMLElement | null>(null)

async function copy(what: 'token' | 'prompt') {
  const text = what === 'token' ? props.created.token : props.created.prompt
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
  emit('close')
}
</script>

<template>
  <div class="note secret" role="region" :aria-label="created.renewed ? 'New secret prompt' : 'New connection prompt'">
    <span class="bold">Prompt for {{ created.connection.name }}</span>
    <span class="sub">Shelf shows it once. <template v-if="created.renewed">The old secret no longer works. </template>The prompt holds the connection's secret, so paste it only into the agent. The secret reaches tags, lyrics, file names, history and authors.</span>
    <pre ref="promptEl" class="mono prompt">{{ created.prompt }}</pre>
    <span class="sub">The secret alone, for SHELF_AGENT_TOKEN</span>
    <code ref="tokenEl" class="mono tok">{{ created.token }}</code>
    <div class="line g10 wrap">
      <button type="button" class="key sm sun" @click="copy('prompt')">Copy prompt</button>
      <button type="button" class="key sm" @click="copy('token')">Copy secret</button>
      <span v-if="copyNote" class="sub" role="status">{{ copyNote }}</span>
      <span class="grow"></span>
      <template v-if="closing">
        <span class="sub" role="alert">Nothing copied yet, and Shelf shows it only once.</span>
        <button type="button" class="key sm" @click="emit('close')">Close anyway</button>
      </template>
      <button v-else type="button" class="key sm" @click="done">Done</button>
    </div>
  </div>
</template>

<style scoped>
.rs .secret { flex-direction: column; align-items: stretch; gap: 10px; border-color: var(--sun); }
.rs .tok { display: block; padding: 8px 10px; overflow-wrap: anywhere; background: var(--field); border: 1px solid var(--ink); border-radius: 5px; color: var(--text); user-select: all; }
.rs .prompt { max-height: 230px; margin: 0; padding: 10px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; background: var(--field); border: 1px solid var(--ink); border-radius: 5px; color: var(--text); }
.rs .smod .key.sm { min-height: 46px; }
</style>
