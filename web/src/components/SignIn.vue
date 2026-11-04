<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { errorText } from '../api/client'
import { useNow } from '../lib/clock'
import { fillDigits, nextStep, startStep, type Step } from '../lib/signin'
import { usePhone } from '../lib/viewport'
import { useStatus } from '../stores/status'
import Icon from './Icon.vue'
import Meter from './Meter.vue'

const emit = defineEmits<{ close: [] }>()

const pollEvery = 1000
const codeWindow = 60
const phone = usePhone()
const now = useNow()
const status = useStatus()

const step = ref<Step>(startStep(status.wrapper))
const username = ref('')
const password = ref('')
const digits = ref<string[]>(['', '', '', '', '', ''])
const boxes = ref<HTMLInputElement[]>([])
const failure = ref('')
const problem = ref('')
const sending = ref(false)

const busy = computed(() => step.value === 'working' || step.value === 'code')
const code = computed(() => digits.value.join(''))
const left = computed(() => {
  const d = status.wrapper?.code_deadline
  return d ? Math.max(0, Math.ceil((Date.parse(d) - now.value) / 1000)) : codeWindow
})
const countdown = computed(() => `${Math.floor(left.value / 60)}:${String(left.value % 60).padStart(2, '0')}`)
const stepLabel = computed(() => (step.value === 'code' ? 'Step 2 of 2' : 'Step 1 of 2'))
const storefront = computed(() => (status.wrapper?.storefront ?? '').toUpperCase())

watch(
  () => status.wrapper,
  (w) => {
    if (!w) return
    const next = nextStep(step.value, w)
    if (next === step.value) return
    if (next === 'failed') failure.value = w.error
    if (next === 'done') password.value = ''
    step.value = next
  },
)

watch(left, (n) => {
  if (step.value === 'code' && n === 0 && status.wrapper?.code_deadline) step.value = 'timeout'
})

let timer: ReturnType<typeof setInterval> | undefined
watch(
  busy,
  (on) => {
    clearInterval(timer)
    if (on) timer = setInterval(() => void status.loadWrapper(), pollEvery)
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  clearInterval(timer)
  password.value = ''
})

async function signIn() {
  sending.value = true
  problem.value = ''
  step.value = 'working'
  try {
    status.applyWrapper(await status.login(username.value, password.value))
  } catch (err) {
    step.value = 'credentials'
    problem.value = errorText(err)
  } finally {
    sending.value = false
  }
}

async function confirm() {
  sending.value = true
  problem.value = ''
  try {
    status.applyWrapper(await status.sendCode(code.value))
  } catch (err) {
    problem.value = errorText(err)
  } finally {
    sending.value = false
  }
}

function typed(at: number, e: Event) {
  const el = e.target as HTMLInputElement
  const r = fillDigits(digits.value, at, el.value)
  digits.value = r.digits
  el.value = r.digits[at] ?? ''
  if (el.value) void nextTick(() => boxes.value[r.focus]?.focus())
}

function pasted(at: number, e: ClipboardEvent) {
  e.preventDefault()
  const r = fillDigits(digits.value, at, e.clipboardData?.getData('text') ?? '')
  digits.value = r.digits
  void nextTick(() => boxes.value[r.focus]?.focus())
}

function back(at: number, e: KeyboardEvent) {
  if (e.key === 'Backspace' && !digits.value[at] && at > 0) boxes.value[at - 1]?.focus()
}

// The wrapper can't send a code again, so a new code means a new sign-in.
async function again() {
  problem.value = ''
  digits.value = ['', '', '', '', '', '']
  if (busy.value) await status.restart().catch(() => {})
  step.value = 'credentials'
}

function tryAgain() {
  if (step.value === 'failed') password.value = ''
  failure.value = ''
  void again()
}

async function close() {
  if (busy.value) await status.restart().catch(() => {})
  emit('close')
}

function escape(e: KeyboardEvent) {
  if (e.key === 'Escape') void close()
}
onMounted(() => document.addEventListener('keydown', escape))
onBeforeUnmount(() => document.removeEventListener('keydown', escape))
</script>

<template>
  <div :class="phone ? 'stack' : 'scrim'">
    <section class="win" :class="{ dlg: !phone }" role="dialog" aria-modal="true" aria-label="Apple Music sign-in">
      <div class="tb">
        <button v-if="phone" type="button" class="wbtn" aria-label="Back to Status" @click="close"><Icon name="back" /></button>
        <h2>Apple Music sign-in</h2>
        <span class="stripes"></span>
        <button v-if="!phone" type="button" class="wbtn" aria-label="Close" @click="close"><Icon name="close" /></button>
      </div>
      <div class="vscroll body">
        <form v-if="step === 'credentials'" class="col g16" @submit.prevent="signIn">
          <span class="px t3 small-h">{{ stepLabel }}</span>
          <h3 class="ask">Sign the wrapper in to an Apple ID</h3>
          <label class="lbl-f">Apple ID<input v-model="username" class="input" type="email" autocomplete="username" required /></label>
          <label class="lbl-f">Password<input v-model="password" class="input" type="password" autocomplete="current-password" required /></label>
          <p class="t2 sm">Shelf passes the password to the wrapper and keeps no copy.</p>
          <p v-if="problem" class="bad-text sm" role="alert">{{ problem }}</p>
          <button type="submit" class="key sun lg block" :disabled="sending || !username || !password">Sign in</button>
        </form>

        <div v-else-if="step === 'working'" class="col g12">
          <div class="lcd lcd-state"><div class="lcd-l line g10"><span class="led run blink"></span>SIGNING IN</div></div>
          <p class="t2 center">Apple is checking the Apple ID.</p>
        </div>

        <form v-else-if="step === 'code'" class="col g16" @submit.prevent="confirm">
          <span class="px t3 small-h">{{ stepLabel }}</span>
          <h3 class="ask">Enter the code sent to your Apple devices</h3>
          <fieldset class="plain-set">
            <legend class="sr">Six-digit code</legend>
            <div class="digits">
              <input v-for="(d, i) in digits" :key="i" ref="boxes" class="digit" inputmode="numeric" :autocomplete="i === 0 ? 'one-time-code' : 'off'" :aria-label="`Digit ${i + 1}`" :value="d" @input="typed(i, $event)" @paste="pasted(i, $event)" @keydown="back(i, $event)" />
            </div>
          </fieldset>
          <div class="line g12">
            <span class="vfd num">{{ countdown }}</span>
            <Meter v-if="!phone" class="grow" :done="Math.ceil(left / 6)" :failed="0" :total="10" :running="false" :label="`${left} of ${codeWindow} seconds left`" />
            <span v-else class="t2 sm">left to enter the code</span>
          </div>
          <p class="t2 sm">Pasting the whole code into the first box fills all six.</p>
          <p v-if="problem" class="bad-text sm" role="alert">{{ problem }}</p>
          <button type="submit" class="key sun lg block" :disabled="sending || code.length !== 6">Confirm</button>
          <button type="button" class="key flat block" @click="again">Send a new code</button>
        </form>

        <div v-else-if="step === 'done'" class="col g16">
          <div class="lcd lcd-state"><div class="lcd-big">SIGNED IN</div><div class="lcd-l">STOREFRONT {{ storefront }}</div></div>
          <p class="center bold">Signed in, storefront {{ storefront }}</p>
          <p class="t2 sm center">Apple Music can search and download again.</p>
          <button type="button" class="key sun block" @click="emit('close')">Done</button>
        </div>

        <div v-else-if="step === 'timeout'" class="col g12">
          <div class="lcd lcd-state"><div class="lcd-big num">0:00</div><div class="lcd-l">CODE TIMED OUT</div></div>
          <p class="center bold">The code wasn't entered in time. Try again.</p>
          <p class="t2 sm center">The wrapper waits 60 seconds for the code.</p>
          <div class="line g12"><button type="button" class="key grow" @click="emit('close')">Close</button><button type="button" class="key sun grow" @click="tryAgain">Try again</button></div>
        </div>

        <div v-else class="col g12">
          <div class="lcd lcd-state"><div class="lcd-big">ERROR</div><div class="lcd-l">SIGN-IN FAILED</div></div>
          <div class="term">{{ failure }}</div>
          <div class="line g12"><button type="button" class="key grow" @click="emit('close')">Close</button><button type="button" class="key sun grow" @click="tryAgain">Try again</button></div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.rs .scrim { position: absolute; inset: 0; z-index: 40; display: flex; align-items: center; justify-content: center; padding: 16px; background: var(--scrim); }
.rs .dlg { width: 100%; max-width: 440px; }
.rs .dlg-body { display: flex; flex-direction: column; gap: 14px; padding: 18px; }
.rs .dlg-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 12px; padding: 0 18px 18px; }
.rs .digits { display: flex; gap: 8px; }
.rs .digit { width: 44px; height: 56px; padding: 0; text-align: center; background: var(--field); border: 1px solid var(--ink); border-radius: 6px; box-shadow: inset 1px 2px 4px rgba(0, 0, 0, .45); font: 600 26px var(--font); color: var(--text); }
.rs .digit.focus { outline: 3px solid var(--focus); outline-offset: 2px; }
.rs .term { margin: 0; padding: 10px 12px; overflow: hidden; background: #07090C; border: 1px solid var(--ink); border-radius: 5px; box-shadow: inset 1px 2px 6px rgba(0, 0, 0, .9); color: #CFE9C9; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; white-space: pre-wrap; }
.rs .term .ts { color: #8F93A8; }
.rs .term .ok { color: #7EE2A8; }
.rs .term .wn { color: #FFD15C; }
.rs .term .er { color: #FF8F8F; }
.rs .scrim { position: fixed; }
.rs .vfd { display: inline-flex; align-items: center; height: 32px; padding: 0 10px; background: var(--lcd); border-radius: 4px; box-shadow: inset 1px 2px 4px rgba(0, 0, 0, .8); color: var(--lcd-text); font: 400 18px/1 var(--disp); text-shadow: 0 0 6px var(--lcd-glow); }
.rs .body { gap: 14px; padding: 18px 14px; }
.rs .ask { font-size: 18px; font-weight: 700; line-height: 1.25; }
.rs .plain-set { margin: 0; padding: 0; border: 0; }
.rs .digits { justify-content: space-between; }
</style>
