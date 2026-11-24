import type { Directive } from 'vue'

// v-vars sets CSS variables from script through the CSSOM instead of style
// attributes in markup.
export const vVars: Directive<HTMLElement, Record<string, string>> = {
  mounted: apply,
  updated: apply,
}

function apply(el: HTMLElement, binding: { value: Record<string, string> }) {
  for (const [k, v] of Object.entries(binding.value)) el.style.setProperty(k, v)
}
