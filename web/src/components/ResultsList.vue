<script setup lang="ts">
import type { Item } from '../api/types'
import { duration } from '../lib/format'
import { downloadable, itemKey, rowLine } from '../lib/rows'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import ItemAction from './ItemAction.vue'
import ItemTitle from './ItemTitle.vue'
import StateLine from './StateLine.vue'
import Tapes from './Tapes.vue'

defineProps<{ items: Item[]; label: string }>()
const emit = defineEmits<{ open: [Item] }>()
const sources = useSources()
const downloads = useDownloads()
const coverSize = (it: Item) => (it.ref.kind === 'track' ? 48 : it.ref.kind === 'artist' ? 56 : 62)
</script>

<template>
  <div class="rows" role="list" :aria-label="label">
    <div v-for="it in items" :key="itemKey(it)" class="row g-ph" :class="`k-${it.ref.kind}`" role="listitem">
      <Cover :url="it.cover_url" :size="coverSize(it)" :round="it.ref.kind === 'artist'" />
      <button type="button" class="two ttl-btn open" @click="emit('open', it)">
        <span class="ttl"><ItemTitle :title="it.title" :version="it.version" :explicit="it.explicit" /></span>
        <span class="sub line g8"><span class="ell">{{ rowLine(it) }}</span><span class="src">{{ sources.name(it.ref.source) }}</span></span>
        <span v-if="downloadable(it)" class="line g10 extra">
          <Tapes :quality="it.quality" />
          <span v-if="it.ref.kind === 'track'" class="t2 num sm">{{ duration(it.duration) }}</span>
          <StateLine :status="downloads.liveStatus(it.ref, it.status)" />
        </span>
      </button>
      <ItemAction v-if="downloadable(it)" :item-ref="it.ref" :title="it.title" :status="it.status" :cover="it.cover_url" layout="phone" />
      <button v-else type="button" class="key flat icon go" :aria-label="`Open ${it.title} on ${sources.name(it.ref.source)}`" @click="emit('open', it)"><Icon name="next" /></button>
    </div>
  </div>
</template>

<style scoped>
.rs .open { gap: 3px; color: inherit; }
.rs .extra { flex-wrap: wrap; row-gap: 4px; margin-top: 4px; }
.rs .g-ph.k-track { grid-template-columns: 48px minmax(0, 1fr) 50px; }
.rs .g-ph.k-artist { grid-template-columns: 56px minmax(0, 1fr) 50px; }
.rs .key.go { width: 50px; height: 50px; }
</style>
