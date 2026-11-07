<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { errorText } from '../api/client'
import { addPasskey, passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { accountCall, useAuth, type Passkey } from '../stores/auth'

const auth = useAuth()
const keys = ref<Passkey[]>([])
const current = ref('')
const password = ref('')
const confirm = ref('')
const keyName = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)
const passkeys = supportsPasskeys()
async function load() { keys.value = (await accountCall<{ passkeys: Passkey[] }>('/passkeys', undefined, 'GET')).passkeys }
onMounted(() => { if (auth.provider === 'simple') void run(load) })
watch(() => auth.account?.id, (id) => { if (id) void run(load) })
async function run(fn: () => Promise<void>) {
  error.value = ''; notice.value = ''; busy.value = true
  try { await fn() } catch (e) { error.value = e instanceof DOMException && e.name === 'NotAllowedError' ? 'Passkey registration was canceled.' : errorText(e) }
  finally { busy.value = false }
}
async function verify() {
  await accountCall('/verify', { password: current.value }); current.value = ''; notice.value = 'Password confirmed for five minutes.'
}
async function changePassword() {
  if (password.value !== confirm.value) throw new Error('Passwords do not match')
  await accountCall('/password', { current_password: current.value, password: password.value })
  current.value = ''; password.value = ''; confirm.value = ''; auth.expired()
}
async function add() { await addPasskey(keyName.value); keyName.value = ''; await load() }
async function remove(id: string) { await accountCall(`/passkeys/${encodeURIComponent(id)}`, undefined, 'DELETE'); auth.expired() }
async function rename(key: Passkey) { await accountCall(`/passkeys/${encodeURIComponent(key.id)}/name`, { name: key.name }); notice.value = 'Passkey name saved.' }
</script>

<template>
  <section v-if="auth.provider === 'simple'" class="win">
    <div class="tb"><h2>Account</h2><span class="stripes"></span></div>
    <div class="account-body account-form">
      <p>Signed in as {{ auth.account?.username }}</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <form class="account-form" @submit.prevent="run(changePassword)">
        <input type="text" name="username" :value="auth.account?.username" autocomplete="username" hidden />
        <label>Current password<input v-model="current" type="password" autocomplete="current-password" required /></label>
        <button class="key" type="button" :disabled="busy || !current" @click="run(verify)">Confirm password for passkey changes</button>
        <label>New password<input v-model="password" type="password" autocomplete="new-password" minlength="8" required /></label>
        <label>Confirm new password<input v-model="confirm" type="password" autocomplete="new-password" minlength="8" required /></label>
        <button class="key" type="submit" :disabled="busy">Change password and sign out</button>
      </form>
      <h3>Passkeys</h3>
      <button v-if="passkeys" class="key" type="button" :disabled="busy" @click="run(passkeyLogin)">Confirm with a passkey</button>
      <p v-if="!keys.length">No passkeys added.</p>
      <div v-for="key in keys" :key="key.id" class="account-form">
        <label>Passkey name<input v-model="key.name" maxlength="64" /></label>
        <p class="sm">Added {{ new Date(key.created_at).toLocaleDateString() }}. {{ key.last_used_at ? `Last used ${new Date(key.last_used_at).toLocaleDateString()}.` : 'Not used yet.' }}</p>
        <button class="key" type="button" :disabled="busy" @click="run(() => rename(key))">Save name</button>
        <button class="key" type="button" :disabled="busy" @click="run(() => remove(key.id))">Remove passkey and sign out</button>
      </div>
      <form v-if="passkeys" class="account-form" @submit.prevent="run(add)">
        <label>New passkey name<input v-model="keyName" maxlength="64" required /></label>
        <button class="key" type="submit" :disabled="busy">Add passkey</button>
      </form>
      <p v-else>Passkeys are unavailable in this browser.</p>
      <button class="key" type="button" :disabled="busy" @click="run(auth.logout)">Sign out of Shelf</button>
    </div>
  </section>
</template>
