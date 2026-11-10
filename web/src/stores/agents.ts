import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { listOf, type Connection, type Setup } from '../api/agents'
import { errorText, get, post } from '../api/client'
import { useInbox } from './inbox'

// Agent connections and the setup prompt, for the Agents panel on Status.
export const useAgents = defineStore('agents', () => {
  const connections = ref<Connection[]>([])
  const setup = ref<Setup | null>(null)
  const error = ref('')
  const busy = ref(false)
  // created holds the one-time secret of the connection just made, and the prompt that holds it. Both live only in this page's memory.
  const created = ref<{ connection: Connection; token: string; prompt: string } | null>(null)

  let createSeq = 0

  const inbox = useInbox()
  // Without simple sign-in nobody can be told apart, so connections come from the CLI.
  const canCreate = computed(() => !!inbox.info?.can_create_connections && inbox.info.accounts === 'simple')

  async function load() {
    error.value = ''
    const [c, s] = await Promise.allSettled([get<unknown>('/api/inbox/connections'), get<Setup>('/api/inbox/setup')])
    if (c.status === 'fulfilled') connections.value = listOf<Connection>(c.value, 'connections')
    else error.value = errorText(c.reason)
    if (s.status === 'fulfilled') setup.value = s.value
    else error.value ||= errorText(s.reason)
  }

  async function create(name: string, operations: string[]): Promise<boolean> {
    if (!canCreate.value) {
      error.value = 'Create connections with shelf manage agents add <name>.'
      return false
    }
    const seq = ++createSeq
    busy.value = true
    error.value = ''
    try {
      const answer = await post<{ connection: Connection; token: string; prompt: string }>('/api/inbox/connections', { name, operations })
      if (seq === createSeq) created.value = answer
      await load()
      void inbox.detect()
      return true
    } catch (err) {
      error.value = errorText(err)
      return false
    } finally {
      busy.value = false
    }
  }

  async function revoke(id: number): Promise<boolean> {
    busy.value = true
    error.value = ''
    try {
      await post(`/api/inbox/connections/${id}/revoke`)
      if (created.value?.connection.id === id) created.value = null
      await load()
      return true
    } catch (err) {
      error.value = errorText(err)
      return false
    } finally {
      busy.value = false
    }
  }

  function forget() {
    // Leaving the panel also forgets a secret whose request has not answered yet.
    ++createSeq
    created.value = null
  }

  return { connections, setup, error, busy, created, canCreate, load, create, revoke, forget }
})
