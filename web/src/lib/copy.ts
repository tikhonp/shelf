// copyText puts text on the clipboard. Without clipboard access (plain HTTP, an old browser) it selects the element so the user can copy by hand.
export async function copyText(text: string, fallback?: HTMLElement | null): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    if (fallback) {
      const range = document.createRange()
      range.selectNodeContents(fallback)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      sel?.addRange(range)
    }
    return false
  }
}
