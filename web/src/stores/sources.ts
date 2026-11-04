import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { get } from '../api/client'
import type { Format, Source, SourceEvent } from '../api/types'
import { defaultFormat } from '../lib/formats'

export const useSources = defineStore('sources', () => {
  const list = ref<Source[]>([])
  const loaded = ref(false)

  async function load() {
    list.value = await get<Source[]>('/api/sources')
    loaded.value = true
  }

  function applyEvent(e: SourceEvent) {
    const s = list.value.find((x) => x.id === e.id)
    if (!s) return
    s.health = { ...s.health, ok: e.ok, message: e.message, detail: e.wrapper ?? s.health.detail }
  }

  const byId = (id: string) => list.value.find((s) => s.id === id)
  const name = (id: string) => byId(id)?.name ?? id
  const formats = (id: string): Format[] => byId(id)?.formats ?? []
  const defaultOf = (id: string) => defaultFormat(formats(id))

  // formatLabel reads a download's format, which may lack the source's prefix.
  function formatLabel(sourceId: string, format: string): string {
    const list = formats(sourceId)
    const f = list.find((x) => x.id === format || x.id === `${sourceId}:${format}`) ?? (format ? undefined : defaultFormat(list))
    return f?.label ?? format
  }

  const enabled = computed(() => list.value.filter((s) => s.enabled))
  const ready = computed(() => enabled.value.filter((s) => s.health.ok))
  // A source that is on and failing needs attention. One that was never set up
  // doesn't, unless no source is ready at all.
  const failing = computed(() => enabled.value.filter((s) => !s.health.ok))
  const noneReady = computed(() => loaded.value && ready.value.length === 0)

  return { list, loaded, load, applyEvent, byId, name, formats, defaultOf, formatLabel, enabled, ready, failing, noneReady }
})
