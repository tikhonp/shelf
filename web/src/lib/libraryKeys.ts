import { isTyping } from './cursor'

export type LibraryAction =
  | 'search' | 'filter' | 'history' | 'guide' | 'keys' | 'all-fields' | 'cover' | 'revert' | 'add-value' | 'note'
  | 'suggest-1' | 'suggest-2' | 'suggest-3' | 'track-prev' | 'track-next' | 'track-all' | 'play' | 'back' | 'forward'
  | 'save' | 'discard' | 'fix' | 'leave' | 'edit' | 'row-up' | 'row-down' | 'open' | 'select-row' | 'bulk'

const plain: Record<string, LibraryAction> = {
  '/': 'search', f: 'filter', h: 'history', g: 'guide', '?': 'keys', a: 'all-fields', c: 'cover', r: 'revert', '+': 'add-value', n: 'note',
  '1': 'suggest-1', '2': 'suggest-2', '3': 'suggest-3', '[': 'track-prev', ']': 'track-next', '0': 'track-all', ' ': 'play',
  ArrowLeft: 'back', ArrowRight: 'forward', ArrowUp: 'row-up', ArrowDown: 'row-down', i: 'fix', Enter: 'edit', x: 'select-row', b: 'bulk',
}

// libraryAction reads a key press. While a field has focus only Save, Discard and
// Esc count, because every other key is typing.
export function libraryAction(e: KeyboardEvent): LibraryAction | null {
  const mod = e.ctrlKey || e.metaKey
  if (mod && e.shiftKey && e.key.toLowerCase() === 'd') return 'discard'
  if (mod && e.key.toLowerCase() === 's') return 'save'
  if (e.key === 'Escape') return 'leave'
  if (mod || e.altKey || isTyping(e)) return null
  // A pressed button takes Enter and Space for itself.
  if ((e.key === 'Enter' || e.key === ' ') && (e.target as HTMLElement | null)?.tagName === 'BUTTON') return null
  return plain[e.key.length === 1 ? e.key.toLowerCase() : e.key] ?? null
}

export const keyGroups: { name: string; keys: { what: string; keys: string[] }[] }[] = [
  { name: 'List', keys: [
    { what: 'Move the row cursor', keys: ['↑', '↓'] },
    { what: 'Open the album', keys: ['Enter'] },
    { what: 'Select the row', keys: ['X'] },
    { what: 'Search the list', keys: ['/'] },
    { what: 'Open the filter', keys: ['F'] },
  ] },
  { name: 'Views', keys: [
    { what: 'History', keys: ['H'] },
    { what: 'Bulk run', keys: ['B'] },
    { what: 'Tagging guide', keys: ['G'] },
    { what: 'This sheet', keys: ['?'] },
  ] },
  { name: 'Fields', keys: [
    { what: 'Next field', keys: ['Tab'] },
    { what: 'Previous field', keys: ['Shift', 'Tab'] },
    { what: 'Edit the field', keys: ['Enter'] },
    { what: 'Leave the field', keys: ['Esc'] },
    { what: 'Revert the field', keys: ['R'] },
    { what: 'Add a value', keys: ['+'] },
    { what: 'Note on the field', keys: ['N'] },
    { what: 'All fields', keys: ['A'] },
    { what: 'Open the cover', keys: ['C'] },
  ] },
  { name: 'Suggestions', keys: [{ what: 'Accept the first, second or third', keys: ['1', '2', '3'] }] },
  { name: 'Tracks', keys: [
    { what: 'Previous or next track', keys: ['[', ']'] },
    { what: 'All tracks', keys: ['0'] },
    { what: 'Play or pause', keys: ['Space'] },
    { what: 'Back or forward 10 seconds', keys: ['←', '→'] },
  ] },
  { name: 'Saving', keys: [
    { what: 'Save', keys: ['Ctrl', 'S'] },
    { what: 'Discard', keys: ['Ctrl', 'Shift', 'D'] },
    { what: 'Fix the first open issue', keys: ['I'] },
  ] },
]
