<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ApiError, errorText } from '../api/client'
import { addPasskey, passkeyLogin, supportsPasskeys } from '../lib/passkeys'
import { shortDay } from '../lib/format'
import { usePhone } from '../lib/viewport'
import { accountCall, useAuth, type Passkey } from '../stores/auth'
import { useEditor } from '../stores/editor'
import KeySwitch from './KeySwitch.vue'
import LibTip from './LibTip.vue'
import PartCard from './PartCard.vue'

const auth = useAuth()
const editor = useEditor()
const phone = usePhone()
// The sign-out key: on while signed in, half turned while it asks about staged edits, off once pressed.
const key = ref<'on' | 'half' | 'off'>('on')
const keys = ref<Passkey[]>([])
const current = ref('')
const password = ref('')
const confirm = ref('')
const proof = ref('')
const keyName = ref('')
// renaming is the passkey whose name is open for editing, and newName its draft.
const renaming = ref('')
const newName = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)
// part is the unit whose action spoke last, so its error or notice shows there.
const part = ref<'account' | 'passkeys' | 'password'>('passkeys')
// at is the passkey the confirm belongs to, or 'add' for a new one, so it opens beside it.
const pending = ref<{ at: string; question: string; sub?: string; label: string; action: () => Promise<void> } | null>(null)
const passkeys = supportsPasskeys()
const lines = computed(() => [auth.account?.username?.toUpperCase() ?? '', `${keys.value.length} ${keys.value.length === 1 ? 'PASSKEY' : 'PASSKEYS'}`])

function signOut() {
  if (key.value !== 'on') return
  if (editor.kept > 0) key.value = 'half'
  else leave()
}
function leave() {
  key.value = 'off'
  void run(async () => {
    try { await auth.logout() } catch (e) { key.value = 'on'; throw e }
  }, 'account')
}
async function load() { keys.value = (await accountCall<{ passkeys: Passkey[] }>('/passkeys', undefined, 'GET')).passkeys }
onMounted(() => { if (auth.provider === 'simple') void run(load) })
watch(() => auth.account?.id, (id) => { if (id) void run(load) })
// passkeyError puts the browser's passkey failures in Shelf's words. InvalidStateError means
// the authenticator already holds a passkey for this account.
function passkeyError(e: unknown): string {
  if (e instanceof DOMException && e.name === 'NotAllowedError') return 'Passkey prompt was canceled.'
  if (e instanceof DOMException && e.name === 'InvalidStateError') return 'This device already has a passkey for this account. Add one from another device.'
  return errorText(e)
}
async function run(fn: () => Promise<void>, at: typeof part.value = 'passkeys') {
  error.value = ''; notice.value = ''; busy.value = true; part.value = at
  try { await fn() } catch (e) { error.value = passkeyError(e) }
  finally { busy.value = false }
}
async function attempt(ask: Omit<NonNullable<typeof pending.value>, 'action'>, action: () => Promise<void>) {
  await run(async () => {
    try { await action() } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 403) throw e
      proof.value = ''
      pending.value = { ...ask, action }
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
function cancel() { pending.value = null; proof.value = '' }
async function changePassword() {
  if (password.value !== confirm.value) throw new Error('Passwords do not match')
  await accountCall('/password', { current_password: current.value, password: password.value })
  current.value = ''; password.value = ''; confirm.value = ''; auth.expired()
}
function startRename(k: Passkey) { renaming.value = k.id; newName.value = k.name; cancel() }
async function add(name: string) { await addPasskey(name); keyName.value = ''; await load() }
async function remove(id: string) { await accountCall(`/passkeys/${encodeURIComponent(id)}`, undefined, 'DELETE'); auth.expired() }
async function rename(id: string, name: string) {
  await accountCall(`/passkeys/${encodeURIComponent(id)}/name`, { name })
  const k = keys.value.find((k) => k.id === id)
  if (k) k.name = name
  renaming.value = ''; notice.value = 'Passkey name saved.'
}
</script>

<template>
  <template v-if="auth.provider === 'simple'">
    <PartCard name="Account" lamp="ok" note="Shelf sign-in" :lines="lines">
      <button class="keyout" type="button" :data-on="key" :disabled="busy" aria-label="Sign out of Shelf" @click="signOut">
        <KeySwitch :width="120" :on="key !== 'off'" :half="key === 'half'" />
        <span class="ko-name" aria-hidden="true">Sign out</span>
      </button>
      <div v-if="key === 'half'" class="note ko-ask" role="group" aria-label="Sign out with staged edits">
        <strong>Sign out with {{ editor.kept }} staged {{ editor.kept === 1 ? 'edit' : 'edits' }}?</strong>
        <span class="sub">They are in the Library and haven't been saved. Signing out drops them.</span>
        <div class="action-row">
          <button class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="key = 'on'">Keep editing</button>
          <button class="key sun" :class="{ sm: !phone }" type="button" :disabled="busy" @click="leave">Sign out</button>
        </div>
      </div>
      <p v-if="error && part === 'account'" class="bad-text" role="alert">{{ error }}</p>
    </PartCard>
    <PartCard name="Passkeys" :lamp="keys.length ? 'ok' : 'off'" :note="phone ? `${keys.length} on this account` : 'Sign in without the password'" :lines="[]">
      <div class="account-content">
        <p v-if="error && part === 'passkeys'" class="bad-text" role="alert">{{ error }}</p>
        <p v-if="notice && part === 'passkeys'" class="t2" role="status">{{ notice }}</p>
        <p v-if="!keys.length">No passkeys added.</p>
        <ul v-else class="passkeys" aria-label="Passkeys">
          <li v-for="k in keys" :key="k.id" class="passkey-row">
            <svg v-if="!phone" width="34" height="34" viewBox="0 0 34 34" aria-hidden="true"><circle cx="11" cy="17" r="8" fill="var(--key)" stroke="var(--key-edge)" stroke-width="1.5" /><circle cx="11" cy="17" r="3" fill="var(--win-2)" /><path d="M19 15h13v4h-3v4h-3v-4h-7z" fill="var(--key)" stroke="var(--key-edge)" stroke-width="1.5" /></svg>
            <form v-if="renaming === k.id" class="pk-rename" @submit.prevent="attempt({ at: k.id, question: `Rename ${k.name}?`, label: 'Rename' }, () => rename(k.id, newName))">
              <label class="lbl-f">Passkey name<input v-model="newName" class="input" maxlength="64" required /></label>
              <div class="pk-keys">
                <button class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="renaming = ''">Cancel</button>
                <button class="key" :class="{ sm: !phone }" type="submit" :disabled="busy">Save</button>
              </div>
            </form>
            <template v-else>
              <span class="pk-t"><span class="pk-name">{{ k.name }}</span><span class="sub">Added {{ shortDay(k.created_at) }} · {{ k.last_used_at ? `used ${shortDay(k.last_used_at)}` : 'not used yet' }}</span></span>
              <div class="pk-keys">
                <button class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="startRename(k)">Rename</button>
                <button class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="renaming = ''; attempt({ at: k.id, question: `Remove ${k.name}?`, sub: 'Removing a passkey signs you out here.', label: 'Remove' }, () => remove(k.id))">Remove</button>
              </div>
            </template>
            <form v-if="pending?.at === k.id" class="note pk-ask" :aria-label="pending.question" @submit.prevent="confirmAction()">
              <div class="pk-ask-t">
                <strong>{{ pending.question }}</strong>
                <span v-if="pending.sub" class="sub">{{ pending.sub }}</span>
                <label class="lbl-f">Password<input v-model="proof" class="input" type="password" autocomplete="current-password" /></label>
              </div>
              <div class="pk-ask-keys">
                <button class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="cancel">Cancel</button>
                <span class="grow"></span>
                <button v-if="passkeys && keys.length" class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="confirmAction(true)">Confirm with a passkey</button>
                <button class="key sun" :class="{ sm: !phone }" type="submit" :disabled="busy || !proof">{{ pending.label }}</button>
              </div>
            </form>
          </li>
        </ul>
        <form v-if="passkeys" class="pk-add" @submit.prevent="attempt({ at: 'add', question: 'Add a passkey?', label: 'Add passkey' }, () => add(keyName))">
          <label class="lbl-f">New passkey name<input v-model="keyName" class="input" maxlength="64" placeholder="Work laptop" required /></label>
          <button class="key" :class="{ block: phone }" type="submit" :disabled="busy">Add passkey</button>
        </form>
        <p v-else>Passkeys are unavailable in this browser.</p>
        <form v-if="pending?.at === 'add'" class="note pk-ask" :aria-label="pending.question" @submit.prevent="confirmAction()">
          <div class="pk-ask-t">
            <strong>{{ pending.question }}</strong>
            <label class="lbl-f">Password<input v-model="proof" class="input" type="password" autocomplete="current-password" /></label>
          </div>
          <div class="pk-ask-keys">
            <button class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="cancel">Cancel</button>
            <span class="grow"></span>
            <button v-if="passkeys && keys.length" class="key" :class="{ sm: !phone }" type="button" :disabled="busy" @click="confirmAction(true)">Confirm with a passkey</button>
            <button class="key sun" :class="{ sm: !phone }" type="submit" :disabled="busy || !proof">{{ pending.label }}</button>
          </div>
        </form>
      </div>
    </PartCard>
    <PartCard name="Password" lamp="ok" note="8 to 128 characters" :lines="[]" last>
      <form class="account-content" @submit.prevent="run(changePassword, 'password')">
        <p v-if="error && part === 'password'" class="bad-text" role="alert">{{ error }}</p>
        <input type="text" name="username" :value="auth.account?.username" autocomplete="username" hidden />
        <div class="pw-fields">
          <label class="lbl-f">Current<input v-model="current" class="input" type="password" autocomplete="current-password" required /></label>
          <label class="lbl-f">New<input v-model="password" class="input" type="password" autocomplete="new-password" minlength="8" required /></label>
          <label class="lbl-f">New, again<input v-model="confirm" class="input" type="password" autocomplete="new-password" minlength="8" required /></label>
        </div>
        <div><button class="key" :class="{ block: phone }" type="submit" :disabled="busy"><LibTip title="Change password" note="Every session ends, this one too, and you sign in again with the new password.">Change password</LibTip></button></div>
      </form>
    </PartCard>
  </template>
</template>

<style scoped>
.rs .account-content { flex: 1 1 560px; min-width: 0; display: flex; flex-direction: column; gap: 14px; }
/* On the phone the unit is a column, so a 560px basis would become a height and stretch the add confirm. */
.rs .smod .account-content { flex: none; }
.rs .action-row { display: flex; flex-wrap: wrap; gap: 10px; }
.rs .ko-ask { flex: 1 1 320px; flex-direction: column; gap: 8px; border-color: var(--sun); }
.rs .passkeys { list-style: none; margin: 0; padding: 0; border-top: 1px solid var(--line); }
.rs .passkey-row { display: flex; flex-wrap: wrap; align-items: center; gap: 14px; padding: 12px 0; border-bottom: 1px solid var(--line); }
.rs .pk-t { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.rs .pk-name { font-weight: 600; overflow-wrap: anywhere; }
.rs .pk-keys { display: flex; gap: 10px; }
.rs .pk-rename { flex: 1; display: flex; flex-wrap: wrap; align-items: flex-end; gap: 10px; }
.rs .pk-rename .lbl-f { flex: 1 1 220px; }
.rs .pk-ask { flex: 1 1 100%; flex-wrap: wrap; align-items: flex-end; gap: 10px; border-color: var(--sun); }
.rs .pk-ask-t { flex: 1 1 260px; display: flex; flex-direction: column; gap: 8px; }
.rs .pk-ask-keys { display: flex; flex-wrap: wrap; gap: 10px; }
.rs .grow { flex: 1; }
.rs .pk-add { display: flex; align-items: flex-end; gap: 12px; }
.rs .pk-add .lbl-f { flex: 1; }
.rs .pw-fields { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.rs .unit .input { height: 40px; }
/* On the phone every row stacks and keys are 46px tall. */
.rs .smod .key { min-height: 46px; }
.rs .smod .passkey-row { flex-direction: column; align-items: stretch; gap: 10px; }
.rs .smod .pk-keys > .key, .rs .smod .action-row > .key { flex: 1; }
.rs .smod .pk-ask-keys { flex-direction: column; width: 100%; }
.rs .smod .pk-ask-keys .grow { display: none; }
.rs .smod .pk-add { flex-direction: column; align-items: stretch; }
.rs .smod .pw-fields { grid-template-columns: 1fr; }
</style>
