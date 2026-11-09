<script setup lang="ts">
import { useAuth } from '../stores/auth'
import { useTheme } from '../stores/theme'

// The switched-off console behind account sign-in, before the app mounts. While Shelf checks access, the status display shows there.
const auth = useAuth()
const theme = useTheme()
</script>

<template>
  <div class="rs desk acct-desk" :class="{ day: theme.day }">
    <header class="acct-bar">
      <span class="acct-brand"><span class="logo" aria-hidden="true"><i></i><i></i><i></i></span>Music Shelf</span>
      <span class="grow"></span>
      <span class="leds" aria-hidden="true"><span class="led off"></span><span class="led off"></span><span class="led off"></span></span>
      <span class="acct-clock" aria-hidden="true">--:--</span>
    </header>
    <section v-if="!auth.ready" class="win acct-check" aria-label="Account access">
      <div class="lcd lcd-state" role="status">
        <span class="lcd-l"><span class="led" :class="auth.error ? 'bad' : 'warn blink'"></span> {{ auth.error ? 'NO ANSWER' : 'CHECKING ACCESS' }}</span>
        <span v-if="auth.error" class="lcd-l">{{ auth.error.toUpperCase() }}</span>
      </div>
      <button v-if="auth.error" class="key sun" type="button" @click="auth.check().catch(() => {})">Try again</button>
    </section>
  </div>
</template>

<style scoped>
.rs.acct-desk { min-height: 100dvh; }
.rs .acct-bar { display: flex; align-items: center; gap: 12px; min-height: 52px; padding: 0 var(--gutter); background: linear-gradient(180deg, #1E232B, #12151A); border-bottom: 1px solid var(--ink); color: #ECE8DF; }
.rs .acct-brand { display: flex; align-items: center; gap: 10px; font-family: var(--label); font-size: 16px; font-weight: 700; letter-spacing: .14em; text-transform: uppercase; white-space: nowrap; }
.rs .logo { display: flex; align-items: flex-end; gap: 2px; flex: none; height: 20px; padding: 2px 3px; background: #07090C; border-radius: 3px; box-shadow: inset 0 1px 2px rgba(0, 0, 0, .8); }
/* Switched off: the brand's level bars stay dark until someone signs in. */
.rs .logo i { display: block; width: 4px; border-radius: 1px; background: #3A4554; }
.rs .logo i:nth-child(1) { height: 8px; }
.rs .logo i:nth-child(2) { height: 12px; }
.rs .logo i:nth-child(3) { height: 15px; }
.rs .acct-bar .leds { min-height: 0; padding: 0; }
.rs .acct-clock { display: inline-flex; align-items: center; height: 32px; padding: 0 10px; background: var(--lcd); border-radius: 4px; box-shadow: inset 1px 2px 4px rgba(0, 0, 0, .8); color: var(--lcd-text); font: 400 18px/1 var(--disp); opacity: .5; }
.rs .acct-check { width: min(440px, calc(100% - 24px)); margin: 15vh auto 0; padding: 16px; gap: 12px; align-items: center; }
.rs .acct-check .lcd { width: 100%; }
.rs .acct-check .lcd-l { display: inline-flex; align-items: center; justify-content: center; gap: 10px; }
@media (max-width: 719px) {
  .rs .acct-bar { padding: 0 12px; }
  .rs .acct-check { margin-top: 12px; }
}
</style>
