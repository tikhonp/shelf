import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { load, save } from '../lib/storage'

export type NoticeLevel = 'off' | 'failures' | 'all'
export type Density = 'comfortable' | 'compact'

const noticesKey = 'shelf.notices'
const densityKey = 'shelf.density'

function read<T extends string>(key: string, allowed: T[], fallback: T): T {
  const v = load(key)
  return allowed.find((a) => a === v) ?? fallback
}

// The choices of this browser. Phone and desktop can differ.
export const useSettings = defineStore('settings', () => {
  // Failures is the default: those downloads need a person, and a finished one
  // already shows in its row and the history.
  const notices = ref<NoticeLevel>(read<NoticeLevel>(noticesKey, ['off', 'failures', 'all'], 'failures'))
  const density = ref<Density>(read<Density>(densityKey, ['comfortable', 'compact'], 'comfortable'))
  watch(notices, (v) => save(noticesKey, v))
  watch(density, (v) => save(densityKey, v))
  return { notices, density }
})
