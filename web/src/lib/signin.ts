import type { Wrapper } from '../api/types'

export type Step = 'credentials' | 'working' | 'code' | 'done' | 'failed' | 'timeout'

// nextStep moves the sign-in on from what the wrapper says. A step that ended
// stays: a failed sign-in's error clears within seconds when the wrapper has an
// old session, and the dialog must keep what it saw.
export function nextStep(step: Step, w: Wrapper): Step {
  if (step === 'credentials' || step === 'done' || step === 'failed' || step === 'timeout') return step
  switch (w.state) {
    case 'awaiting_code':
      return 'code'
    case 'signing_in':
    case 'starting':
      return 'working'
    case 'running':
      return 'done'
    case 'error':
      return /2FA code timeout/.test(w.error) ? 'timeout' : 'failed'
    default:
      return step
  }
}

// startStep is where the dialog opens: a sign-in may already run.
export function startStep(w: Wrapper | null): Step {
  if (w?.state === 'awaiting_code') return 'code'
  return w?.state === 'signing_in' ? 'working' : 'credentials'
}

// joinDigits fills six boxes from what was typed or pasted into box `at`.
export function fillDigits(digits: string[], at: number, text: string): { digits: string[]; focus: number } {
  const out = [...digits]
  const clean = text.replace(/\D/g, '')
  let i = at
  for (const c of clean) {
    if (i > 5) break
    out[i++] = c
  }
  if (clean === '') out[at] = ''
  return { digits: out, focus: Math.min(i, 5) }
}
