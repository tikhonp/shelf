<script setup lang="ts">
import { onMounted, watch } from 'vue'
import { RouterView } from 'vue-router'
import { setAuthHandling } from './api/client'
import AccountLogin from './components/AccountLogin.vue'
import Shell from './components/Shell.vue'
import { useAuth } from './stores/auth'
import { useEvents } from './stores/events'
import { usePlayer } from './stores/player'
const auth = useAuth()
setAuthHandling(auth.expired, () => auth.locked)
onMounted(() => void auth.check().catch(() => {}))
watch(() => auth.locked, (locked) => {
  if (locked) { useEvents().disconnect(); usePlayer().stop() }
  else if (auth.mounted) useEvents().connect()
})
</script>

<template>
  <div v-if="!auth.ready" class="rs account-body" role="status">
    <p>{{ auth.error || 'Checking account access...' }}</p>
    <button v-if="auth.error" class="key" type="button" @click="auth.check().catch(() => {})">Try again</button>
  </div>
  <div v-if="auth.mounted" :inert="auth.locked || undefined" :aria-hidden="auth.locked || undefined">
    <Shell><RouterView /></Shell>
  </div>
  <AccountLogin v-if="auth.locked" />
</template>
