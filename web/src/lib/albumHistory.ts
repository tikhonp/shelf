import { onMounted, ref, watch, type Ref } from 'vue'
import { get } from '../api/client'
import type { Download, DownloadList, LibraryChange } from '../api/types'
import { useEditor } from '../stores/editor'
import { useHistory } from '../stores/history'

// useAlbumHistory loads one album's History and the downloads that wrote into
// its folder, for the desktop window and the phone page. openId is the change
// whose lines show. The phone passes its own, because the Undo slot sits
// outside the page.
export function useAlbumHistory(albumId: () => number, openId: Ref<number | null> = ref(null)) {
  const history = useHistory()
  const editor = useEditor()
  const downloads = ref<Download[]>([])

  onMounted(() => void history.load(albumId()))
  watch(albumId, (id) => {
    openId.value = null
    void history.load(id)
  })

  // The downloads come last. Without any, or when the call fails, History
  // leaves that part out.
  watch(
    () => editor.album?.folder,
    async (folder) => {
      downloads.value = []
      if (!folder) return
      try {
        const page = await get<DownloadList>(`/api/downloads?folder=${encodeURIComponent(folder)}`)
        if (editor.album?.folder === folder) downloads.value = page.downloads
      } catch {
        downloads.value = []
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

  return { openId, downloads, toggle, downloadedAt }
}
