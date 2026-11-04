// isTyping says whether a key belongs to a field, so list keys leave it alone.
export function isTyping(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null
  return !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)
}

// moved is the row cursor after an arrow key, or null for any other key.
export function moved(key: string, cursor: number, rows: number): number | null {
  if (rows === 0) return null
  if (key === 'ArrowDown') return Math.min(rows - 1, cursor + 1)
  if (key === 'ArrowUp') return Math.max(0, cursor - 1)
  return null
}
