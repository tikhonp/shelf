import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { load, save } from '../lib/storage'

const key = 'shelf.theme'

// Night is the default. The desktop's switch is in the menubar and the phone's on Status.
export const useTheme = defineStore('theme', () => {
  const day = ref(load(key) === 'day')
  watch(day, (v) => save(key, v ? 'day' : 'night'))
  return { day }
})
