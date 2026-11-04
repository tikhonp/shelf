<script setup lang="ts">
import { computed } from 'vue'
import type { Source } from '../api/types'
import { sourceLamp, sourceState } from '../lib/lamps'
import { useSearch } from '../stores/search'
import { useSources } from '../stores/sources'
import Icon from './Icon.vue'

// One switch in two places: the button sits in the window's title bar or beside
// the field, and its rockers open in the window's body.
const props = defineProps<{ part: 'button' | 'rockers'; open?: boolean; small?: boolean; panelId: string }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const sources = useSources()
const search = useSearch()
const isOff = (s: Source) => search.off.includes(s.id)

const words = computed(() => {
  const on = search.on
  if (on.length === 0) return 'No sources'
  if (on.length === sources.enabled.length) return 'All sources'
  return on.length === 1 ? `${on[0]!.name} only` : `${on.length} sources`
})
const toggle = () => emit('update:open', !props.open)
</script>

<template>
  <button v-if="part === 'button'" type="button" class="srcbtn" :class="small ? 'wbtn txt' : 'key tall'" :aria-expanded="!!open" :aria-controls="panelId" :aria-label="`Sources: ${words}`" @click="toggle">
    <span>{{ words }}</span>
    <span class="lamps" aria-hidden="true"><span v-for="s in sources.list" :key="s.id" class="led" :class="sourceLamp(s, isOff(s))"></span></span>
    <Icon name="chevron" class="chev" />
  </button>
  <div v-else :id="panelId" class="rockers" role="group" aria-label="Sources to search">
    <button v-for="s in sources.list" :key="s.id" type="button" class="rocker" role="switch" :aria-checked="s.enabled && !isOff(s)" :disabled="!s.enabled" @click="search.toggle(s.id)">
      <span class="rk" aria-hidden="true">ON<span class="rk-case"><span class="rk-paddle"></span></span>OFF</span>
      <span class="rk-t">
        <span class="rk-name">{{ s.name }}</span>
        <span class="rk-state"><span class="led" :class="sourceLamp(s, isOff(s))"></span>{{ sourceState(s, isOff(s)) }}</span>
      </span>
    </button>
    <p v-if="!small" class="t2 sm about">Every source that is on is searched, and the results come in one list.</p>
  </div>
</template>

<style scoped>
.rs .srcbtn .lamps { display: inline-flex; gap: 6px; margin-left: 2px; }
.rs .srcbtn .chev { width: 16px; height: 16px; transition: transform .2s ease; }
.rs .srcbtn[aria-expanded="true"] .chev { transform: rotate(180deg); }
.rs .rockers { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 18px; padding: 8px 10px; background: var(--win-2); border: 1px solid var(--ink); border-radius: 6px; box-shadow: inset 0 1px 3px rgba(0, 0, 0, .35); }
.rs .srcbtn.tall { height: 50px; }
.rs .about { margin-left: auto; max-width: 320px; }
</style>
