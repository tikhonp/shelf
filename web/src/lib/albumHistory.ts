import { nextTick, onMounted, ref, watch, type Ref } from 'vue'
import { errorText, get } from '../api/client'
import type { Download, DownloadList, LibraryChange } from '../api/types'
import { useDownloads } from '../stores/downloads'
import { useEditor } from '../stores/editor'
import { useHistory } from '../stores/history'

// useAlbumHistory loads one album's History and the downloads that wrote into
// its folder, for the desktop window and the phone page. openId is the change
// whose lines show. The phone passes its own, because the Undo slot sits
// outside the page.
export function useAlbumHistory(
  albumId: () => number,
  openId: Ref<number | null> = ref(null),
  changeId?: () => number | null | undefined,
  root?: Ref<HTMLElement | undefined>,
) {
  const history = useHistory()
  const editor = useEditor()
  const downloadsStore = useDownloads()
  const downloads = ref<Download[]>([])
  const missingChange = ref<number | null>(null)
  let focusSeq = 0

  async function focusChange(target: number | null | undefined) {
    const seq = ++focusSeq
    missingChange.value = null
    if (!target || history.error) return
    while (!history.changes.some((c) => c.id === target) && history.next !== null) {
      const prevNext = history.next
      try {
        await history.more()
      } catch (err) {
        if (seq === focusSeq) history.error = errorText(err)
        return
      }
      if (seq !== focusSeq) return
      if (history.next === prevNext) break
    }
    if (!history.changes.some((c) => c.id === target)) {
      if (seq === focusSeq && !history.error) missingChange.value = target
      return
    }
    openId.value = target
    if (!history.details[target]) void history.open(target)
    await nextTick()
    if (seq !== focusSeq) return
    root?.value?.querySelector<HTMLElement>(`[data-change="${target}"]`)?.scrollIntoView?.({ block: 'center' })
  }

  onMounted(async () => {
    await history.load(albumId())
    await focusChange(changeId?.())
  })
  watch(albumId, async (id) => {
    openId.value = null
    await history.load(id)
    await focusChange(changeId?.())
  })
  if (changeId) {
    watch(changeId, (target) => {
      if (target) void focusChange(target)
    })
  }

  // The downloads come last. When downloads are off, without any, or when the
  // call fails, History leaves that part out.
  watch(
    [() => editor.album?.folder, () => downloadsStore.enabled],
    async ([folder, on]) => {
      downloads.value = []
      if (!folder || on !== true) return
      try {
        const page = await get<DownloadList>(`/api/downloads?folder=${encodeURIComponent(folder)}`)
        if (editor.album?.folder === folder && downloadsStore.enabled === true) downloads.value = page.downloads
      } catch {
        // The watcher already cleared downloads before starting this request.
      }
    },
    { immediate: true },
  )

  // A model ref from the phone's page takes its new value only after the
  // parent renders, so toggle reads the next value from a local.
  function toggle(c: LibraryChange) {
    const next = openId.value === c.id ? null : c.id
    openId.value = next
    if (next !== null && !history.details[c.id]) void history.open(c.id)
  }
  const downloadedAt = (d: Download) => d.finished_at ?? d.created_at

  return { openId, downloads, missingChange, toggle, downloadedAt }
}
