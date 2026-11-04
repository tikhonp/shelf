const qobuzSizes = [50, 100, 230, 600]

// coverUrl asks for the size a cell shows, times the device pixel ratio.
export function coverUrl(url: string | undefined, px: number, ratio = window.devicePixelRatio || 1): string {
  if (!url) return ''
  const want = Math.ceil(px * ratio)
  // Apple's artwork URLs end in the size, such as 600x600bb.jpg.
  const apple = url.replace(/\/\d+x\d+(bb)?\.(jpg|jpeg|png|webp)$/, `/${want}x${want}$1.$2`)
  if (apple !== url) return apple
  if (url.includes('static.qobuz.com')) {
    const fit = qobuzSizes.find((s) => s >= want) ?? 600
    return url.replace(/_(\d+|max|org)\.jpg$/, `_${fit}.jpg`)
  }
  return url
}
