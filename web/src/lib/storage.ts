// The browser can refuse storage, such as in a private window, so these never throw.
export function load(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function save(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // The choice lasts until the page reloads.
  }
}
