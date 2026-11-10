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

// useKeyboard gives the height the on-screen keyboard covers at the bottom of the window, or 0.
// iOS doesn't shrink the layout viewport for the keyboard, so only visualViewport shows it.
export function useKeyboard(): Ref<number> {
  const covered = ref(0)
  const vv = window.visualViewport
  if (!vv) return covered
  const update = () => {
    const h = window.innerHeight - vv.height - vv.offsetTop
    // A browser's toolbar changes the height by less than this, so a smaller change is not the keyboard.
    covered.value = h > 120 ? Math.round(h) : 0
  }
  update()
  vv.addEventListener('resize', update)
  vv.addEventListener('scroll', update)
  onScopeDispose(() => { vv.removeEventListener('resize', update); vv.removeEventListener('scroll', update) })
  return covered
}
