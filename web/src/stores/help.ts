import { defineStore } from 'pinia'
import { ref } from 'vue'

export type HelpSection = 'guide' | 'keys'

// What Help has open: the sheet, and on the phone the note in its slot.
export const useHelp = defineStore('help', () => {
  const sheet = ref<HelpSection | null>(null)
  const note = ref<{ title: string; text: string } | null>(null)
  return { sheet, note }
})
