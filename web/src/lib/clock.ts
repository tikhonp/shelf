import { onScopeDispose, ref, type Ref } from 'vue'

// useNow is the time in milliseconds, updated every second for countdowns and
// running times.
export function useNow(): Ref<number> {
  const now = ref(Date.now())
  const timer = setInterval(() => (now.value = Date.now()), 1000)
  onScopeDispose(() => clearInterval(timer))
  return now
}
