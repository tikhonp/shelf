<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { duration } from '../lib/format'
import { usePlayer } from '../stores/player'
import Icon from './Icon.vue'

const player = usePlayer()
const audio = ref<HTMLAudioElement>()
const time = ref(0)
const length = ref(0)

// transcoded is true once the browser refused the file. shelf then streams it as
// MP3, which has no length, so the position can't move.
const transcoded = ref(false)
const src = computed(() => (player.track ? `/api/library/tracks/${player.track.id}/audio${transcoded.value ? '?transcode=1' : ''}` : ''))
const two = (n: number) => String(n).padStart(2, '0')
const title = computed(() => (player.track ? `${two(player.track.track)} ${player.track.title}`.toUpperCase() : ''))
const shownLength = computed(() => (Number.isFinite(length.value) && length.value) || player.track?.duration || 0)
const words = computed(() => `Playing track ${two(player.track?.track ?? 0)}, ${player.track?.title}, ${duration(time.value)} of ${duration(shownLength.value)}`)

watch(
  () => [player.playing, src.value, audio.value] as const,
  ([playing, , el]) => {
    if (!el) return
    if (playing) el.play()?.catch(() => {})
    else el.pause()
  },
  { flush: 'post' },
)
watch(
  () => player.track?.id,
  () => (transcoded.value = false),
)
watch(src, () => {
  time.value = 0
  length.value = 0
})
onBeforeUnmount(() => player.stop())

// skip moves by seconds, for the arrow keys.
function skip(by: number) {
  if (audio.value) audio.value.currentTime = Math.max(0, audio.value.currentTime + by)
}
defineExpose({ skip })

function seek(e: Event) {
  if (audio.value) audio.value.currentTime = Number((e.target as HTMLInputElement).value)
}
function failed() {
  if (!transcoded.value) {
    transcoded.value = true
    return
  }
  player.playing = false
  player.error = "This browser can't play this file."
}
</script>

<template>
  <div v-if="player.track" class="lib-player" role="group" aria-label="Player">
    <audio ref="audio" :src="src" preload="metadata" @timeupdate="time = audio?.currentTime ?? 0" @loadedmetadata="length = audio?.duration ?? 0" @ended="player.playing = false" @error="failed"></audio>
    <div class="lcd" role="img" :aria-label="words">
      <div class="lcd-l line g10"><span class="ell">{{ player.error ? player.error.toUpperCase() : title }}</span><span class="num end">{{ duration(time) }}/{{ duration(shownLength) }}</span></div>
    </div>
    <div class="lib-player-keys">
      <button type="button" class="key sm icon" :aria-label="player.playing ? 'Pause' : 'Play'" @click="player.toggle(player.track)"><Icon :name="player.playing ? 'pause' : 'play'" /></button>
      <input class="lib-scrub" type="range" min="0" :max="Math.ceil(shownLength)" :value="time" :disabled="transcoded" aria-label="Position in the track" @input="seek" />
      <button type="button" class="key sm icon" aria-label="Close the player" @click="player.stop()"><Icon name="close" /></button>
    </div>
  </div>
</template>

<style scoped>
.rs .end { margin-left: auto; flex: none; }
</style>
