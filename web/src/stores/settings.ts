import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { load, save } from '../lib/storage'

export type NoticeLevel = 'off' | 'failures' | 'all'

const key = 'shelf.notices'
const levels: NoticeLevel[] = ['off', 'failures', 'all']

// Failures is the default: those downloads need a person, and a finished one
// already shows in its row and the history.
function readLevel(): NoticeLevel {
  const v = load(key)
  return levels.find((l) => l === v) ?? 'failures'
}

// The choices of this browser. Phone and desktop can differ.
export const useSettings = defineStore('settings', () => {
  const notices = ref<NoticeLevel>(readLevel())
  watch(notices, (v) => save(key, v))
  return { notices }
})
