import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { LibraryTrack } from '../api/types'

// The album's one player. LibPlayer holds the audio element and appears only
// while a track is loaded.
export const usePlayer = defineStore('player', () => {
  const track = ref<LibraryTrack | null>(null)
  const playing = ref(false)
  const error = ref('')

  function toggle(t: LibraryTrack) {
    if (track.value?.id === t.id) playing.value = !playing.value
    else {
      track.value = t
      playing.value = true
      error.value = ''
    }
  }

  function stop() {
    track.value = null
    playing.value = false
    error.value = ''
  }

  return { track, playing, error, toggle, stop }
})
