import { ApiError } from '../api/client'

// The steps of account sign-in, as the display shows them.
export type SignState =
  | 'checking' | 'noanswer' | 'setup' | 'out' | 'ended'
  | 'busy' | 'waiting' | 'wrong' | 'many' | 'canceled' | 'failed' | 'hello'

// tone colors the display: red for a failure, which is also an alert, and amber for a soft stop.
export interface SignDisplay { lines: string[]; tone: '' | 'err' | 'warn' }

// failed is the state a failed sign-in leaves. Shelf words its own failures,
// because the server writes its sentence for logs and API clients.
export function failed(e: unknown): SignState {
  if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) return 'canceled'
  if (e instanceof ApiError && e.status === 401) return 'wrong'
  if (e instanceof ApiError && e.status === 429) return 'many'
  return 'failed'
}

export function edits(n: number): string {
  if (n === 0) return 'SIGN IN TO GO ON'
  return `${n} ${n === 1 ? 'EDIT' : 'EDITS'} KEPT`
}

// display is what the sign-in display reads under its MUSIC SHELF line.
export function display(s: SignState, o: { name?: string; passkeys?: boolean; kept?: number } = {}): SignDisplay {
  const name = (o.name || '').toUpperCase()
  switch (s) {
    case 'checking': return { lines: ['CHECKING ACCESS'], tone: '' }
    case 'noanswer': return { lines: ['NO ANSWER', 'TRY AGAIN'], tone: 'err' }
    case 'setup': return { lines: ['NO ACCOUNTS', 'ADD ONE ON THE SERVER'], tone: '' }
    case 'out': return { lines: ['SIGNED OUT', o.passkeys ? 'PASSWORD OR PASSKEY' : 'PASSWORD'], tone: '' }
    case 'ended': return { lines: ['SESSION ENDED', edits(o.kept ?? 0)], tone: 'warn' }
    case 'busy': return { lines: ['CHECKING', name], tone: '' }
    case 'waiting': return { lines: ['WAITING FOR', 'YOUR PASSKEY'], tone: '' }
    case 'wrong': return { lines: ['WRONG USERNAME', 'OR PASSWORD'], tone: 'err' }
    case 'many': return { lines: ['TOO MANY TRIES', 'TRY AGAIN LATER'], tone: 'err' }
    case 'canceled': return { lines: ['PASSKEY CANCELED', 'USE THE PASSWORD'], tone: 'warn' }
    case 'failed': return { lines: ["CAN'T SIGN IN", 'TRY AGAIN'], tone: 'err' }
    case 'hello': return { lines: [`HELLO, ${name}`, 'OPENING THE LIBRARY'], tone: '' }
  }
}

// reducedMotion says whether the power-up should be skipped.
export const reducedMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
