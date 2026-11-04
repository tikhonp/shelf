<script setup lang="ts">
import { RouterLink } from 'vue-router'
import type { Source } from '../api/types'
import { sourceLamp } from '../lib/lamps'

defineProps<{ source: Source }>()
</script>

<template>
  <section class="panel col g10">
    <div class="smod-h"><span class="led" :class="sourceLamp(source) === 'off' ? 'bad' : sourceLamp(source)"></span><h2>{{ source.name }}</h2><span class="sub">{{ source.enabled ? source.health.message : 'Not set up' }}</span></div>
    <ol v-if="source.id === 'qobuz'" class="steps">
      <li>Sign in at play.qobuz.com in a browser.</li>
      <li>Copy the user auth token from the web player's local storage.</li>
      <li>Put it in <code class="mono">SHELF_QOBUZ_TOKEN</code> and restart shelf.</li>
    </ol>
    <template v-else-if="source.id === 'applemusic' && source.enabled">
      <p class="t2">The wrapper needs an Apple ID before Apple Music can search or download.</p>
      <RouterLink class="key sun block" :to="{ path: '/status', query: { signin: '1' } }">Sign in</RouterLink>
    </template>
    <p v-else class="t2">{{ source.health.message }}</p>
  </section>
</template>

<style scoped>
.rs .steps { display: flex; flex-direction: column; gap: 4px; padding-left: 20px; color: var(--text-2); }
.rs code { color: var(--text); }
</style>
