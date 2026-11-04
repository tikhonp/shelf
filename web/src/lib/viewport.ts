import { onScopeDispose, ref, type Ref } from 'vue'

// The width where the phone layout starts, the same as the media query in the styles.
const phoneQuery = '(max-width: 719px)'

// usePhone says whether the phone layout applies, and follows the window.
export function usePhone(): Ref<boolean> {
  const list = window.matchMedia?.(phoneQuery)
  const phone = ref(list?.matches ?? false)
  if (list) {
    const change = (e: MediaQueryListEvent) => (phone.value = e.matches)
    list.addEventListener('change', change)
    onScopeDispose(() => list.removeEventListener('change', change))
  }
  return phone
}
