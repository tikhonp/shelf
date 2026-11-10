<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ApiError, errorText } from '../api/client'
import { addPasskey, passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { accountCall, useAuth, type Passkey } from '../stores/auth'
import { useEditor } from '../stores/editor'
import PartCard from './PartCard.vue'

const auth = useAuth()
const editor = useEditor()
const confirmLogout = ref(false)
const keys = ref<Passkey[]>([])
const current = ref('')
const password = ref('')
const confirm = ref('')
const proof = ref('')
const keyName = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)
// part is the unit whose action spoke last, so its error or notice shows there.
const part = ref<'account' | 'passkeys' | 'password'>('passkeys')
const pending = ref<{ question: string; label: string; action: () => Promise<void> } | null>(null)
const passkeys = supportsPasskeys()
function signOut() {
  if (editor.kept > 0) confirmLogout.value = true
  else void run(auth.logout, 'account')
}
async function load() { keys.value = (await accountCall<{ passkeys: Passkey[] }>('/passkeys', undefined, 'GET')).passkeys }
onMounted(() => { if (auth.provider === 'simple') void run(load) })
watch(() => auth.account?.id, (id) => { if (id) void run(load) })
async function run(fn: () => Promise<void>, at: typeof part.value = 'passkeys') {
  error.value = ''; notice.value = ''; busy.value = true; part.value = at
  try { await fn() } catch (e) { error.value = e instanceof DOMException && e.name === 'NotAllowedError' ? 'Passkey prompt was canceled.' : errorText(e) }
  finally { busy.value = false }
}
async function attempt(question: string, label: string, action: () => Promise<void>) {
  await run(async () => {
    try { await action() } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 403) throw e
      proof.value = ''
      pending.value = { question, label, action }
    }
  })
}
async function confirmAction(usePasskey = false) {
  await run(async () => {
    const action = pending.value?.action
    if (!action) return
    if (usePasskey) await passkeyLogin()
    else await accountCall('/verify', { password: proof.value })
    await action()
    pending.value = null; proof.value = ''
  })
}
async function changePassword() {
  if (password.value !== confirm.value) throw new Error('Passwords do not match')
  await accountCall('/password', { current_password: current.value, password: password.value })
  current.value = ''; password.value = ''; confirm.value = ''; auth.expired()
}
async function add(name: string) { await addPasskey(name); keyName.value = ''; await load() }
async function remove(id: string) { await accountCall(`/passkeys/${encodeURIComponent(id)}`, undefined, 'DELETE'); auth.expired() }
async function rename(id: string, name: string) { await accountCall(`/passkeys/${encodeURIComponent(id)}/name`, { name }); notice.value = 'Passkey name saved.' }
</script>

<template>
  <template v-if="auth.provider === 'simple'">
    <PartCard name="Account" lamp="ok" :lines="[`SIGNED IN AS ${auth.account?.username?.toUpperCase() ?? ''}`, `${keys.length} ${keys.length === 1 ? 'PASSKEY' : 'PASSKEYS'}`]">
      <template #keys><button class="key" type="button" :disabled="busy" @click="signOut">Sign out of Shelf</button></template>
      <div v-if="confirmLogout" class="note confirm-note">
        <strong>Sign out with {{ editor.kept }} staged {{ editor.kept === 1 ? 'edit' : 'edits' }}?</strong>
        <div class="action-row">
          <button class="key" type="button" :disabled="busy" @click="confirmLogout = false">Keep editing</button>
          <button class="key" type="button" :disabled="busy" @click="run(auth.logout, 'account')">Sign out</button>
        </div>
      </div>
      <p v-if="error && part === 'account'" class="bad-text" role="alert">{{ error }}</p>
    </PartCard>
    <PartCard name="Passkeys" :lines="[]">
      <div class="account-content">
        <p v-if="error && part === 'passkeys'" class="bad-text" role="alert">{{ error }}</p>
        <p v-if="notice && part === 'passkeys'" class="t2" role="status">{{ notice }}</p>
        <p v-if="!keys.length">No passkeys added.</p>
        <div v-for="key in keys" :key="key.id" class="passkey-row">
          <label>Passkey name<input v-model="key.name" class="input" maxlength="64" /></label>
          <p class="sm">Added {{ new Date(key.created_at).toLocaleDateString() }} · {{ key.last_used_at ? `Last used ${new Date(key.last_used_at).toLocaleDateString()}` : 'Not used yet' }}</p>
          <button class="key" type="button" :disabled="busy" @click="attempt(`Rename ${key.name}?`, 'Rename', () => rename(key.id, key.name))">Rename</button>
          <button class="key" type="button" :disabled="busy" @click="attempt(`Remove ${key.name}?`, 'Remove', () => remove(key.id))">Remove</button>
        </div>
        <p v-if="keys.length" class="sm">Removing a passkey signs you out.</p>
        <form v-if="passkeys" class="action-row" @submit.prevent="attempt('Add a passkey?', 'Add passkey', () => add(keyName))">
          <label>New passkey name<input v-model="keyName" class="input" maxlength="64" required /></label>
          <button class="key" type="submit" :disabled="busy">Add passkey</button>
        </form>
        <p v-else>Passkeys are unavailable in this browser.</p>
        <form v-if="pending" class="note confirm-note" @submit.prevent="confirmAction()">
          <strong>{{ pending.question }}</strong>
          <label>Password<input v-model="proof" class="input" type="password" autocomplete="current-password" required /></label>
          <div class="action-row">
            <button v-if="passkeys" class="key" type="button" :disabled="busy" @click="confirmAction(true)">Use a passkey</button>
            <button class="key" type="submit" :disabled="busy">{{ pending.label }}</button>
            <button class="key" type="button" :disabled="busy" @click="pending = null; proof = ''">Cancel</button>
          </div>
        </form>
      </div>
    </PartCard>
    <PartCard name="Password" :lines="[]">
      <form class="account-content" @submit.prevent="run(changePassword, 'password')">
        <p v-if="error && part === 'password'" class="bad-text" role="alert">{{ error }}</p>
        <input type="text" name="username" :value="auth.account?.username" autocomplete="username" hidden />
        <div class="action-row">
          <label>Current password<input v-model="current" class="input" type="password" autocomplete="current-password" required /></label>
          <label>New password<input v-model="password" class="input" type="password" autocomplete="new-password" minlength="8" required /></label>
          <label>New again<input v-model="confirm" class="input" type="password" autocomplete="new-password" minlength="8" required /></label>
        </div>
        <div class="action-row"><button class="key" type="submit" :disabled="busy">Change password</button><span class="t2 sm">Then you sign in again with the new one.</span></div>
      </form>
    </PartCard>
  </template>
</template>

<style scoped>
.rs .account-content { flex: 1 1 400px; min-width: 0; display: flex; flex-direction: column; gap: 12px; }
.rs .action-row, .rs .passkey-row { display: flex; flex-wrap: wrap; align-items: end; gap: 10px; }
.rs label { display: flex; flex-direction: column; gap: 4px; }
.rs .confirm-note { display: flex; flex-wrap: wrap; align-items: end; gap: 10px; }
</style>
