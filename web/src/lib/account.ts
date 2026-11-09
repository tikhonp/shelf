import { ApiError } from '../api/client'

// The steps of account sign-in, as the display and the lamp show them.
export type SignState =
  | 'checking' | 'noanswer' | 'setup' | 'out' | 'ended'
  | 'busy' | 'waiting' | 'wrong' | 'many' | 'canceled' | 'failed' | 'hello'

export interface SignDisplay { lines: string[]; lamp: string }

// failed is the state a failed sign-in leaves. Shelf words its own failures,
// because the server writes its sentence for logs and API clients.
export function failed(e: unknown): SignState {
  if (e instanceof DOMException && e.name === 'NotAllowedError') return 'canceled'
  if (e instanceof ApiError && e.status === 401) return 'wrong'
  if (e instanceof ApiError && e.status === 429) return 'many'
  return 'failed'
}

export function edits(n: number): string {
  if (n === 0) return 'SIGN IN TO GO ON'
  return `${n} ${n === 1 ? 'EDIT' : 'EDITS'} KEPT`
}

// display is what the sign-in display reads, with the lamp beside it.
export function display(s: SignState, o: { name?: string; passkeys?: boolean; kept?: number } = {}): SignDisplay {
  const name = (o.name || '').toUpperCase()
  switch (s) {
    case 'checking': return { lines: ['CHECKING ACCESS'], lamp: 'warn blink' }
    case 'noanswer': return { lines: ['NO ANSWER', 'TRY AGAIN'], lamp: 'bad' }
    case 'setup': return { lines: ['NO ACCOUNTS', 'ADD ONE ON THE SERVER'], lamp: 'warn' }
    case 'out': return { lines: ['SIGNED OUT', o.passkeys ? 'PASSWORD OR PASSKEY' : 'PASSWORD'], lamp: 'off' }
    case 'ended': return { lines: ['SESSION ENDED', edits(o.kept ?? 0)], lamp: 'warn' }
    case 'busy': return { lines: ['CHECKING', name], lamp: 'warn blink' }
    case 'waiting': return { lines: ['WAITING FOR', 'YOUR PASSKEY'], lamp: 'warn blink' }
    case 'wrong': return { lines: ['WRONG USERNAME', 'OR PASSWORD'], lamp: 'bad' }
    case 'many': return { lines: ['TOO MANY TRIES', 'TRY AGAIN LATER'], lamp: 'bad' }
    case 'canceled': return { lines: ['PASSKEY CANCELED', 'USE THE PASSWORD'], lamp: 'warn' }
    case 'failed': return { lines: ["CAN'T SIGN IN", 'TRY AGAIN'], lamp: 'bad' }
    case 'hello': return { lines: [`HELLO, ${name}`, 'OPENING THE LIBRARY'], lamp: 'ok' }
  }
}

// reducedMotion says whether the power-up should be skipped.
export const reducedMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
