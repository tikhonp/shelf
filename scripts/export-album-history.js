// Renders the album History boards from the design canvas to static pages in docs/ui/design/screens.
// Run from the repo root: NODE_PATH=web/node_modules node scripts/export-album-history.js docs/ui/design/boards/album-history docs/ui/design/screens
const fs = require('fs')
const path = require('path')
const { JSDOM } = require('jsdom')

const [boardsDir, outDir] = process.argv.slice(2)

class DCLogic {
  constructor(props) { this.props = props || {}; this.state = {} }
  setState() {}
}

function load(name) {
  const src = fs.readFileSync(path.join(boardsDir, name + '.dc.html'), 'utf8')
  const dom = new JSDOM(src)
  const doc = dom.window.document
  const xdc = doc.querySelector('x-dc')
  const helmet = xdc.querySelector('helmet')
  const style = helmet ? [...helmet.querySelectorAll('style')].map((s) => s.textContent).join('\n') : ''
  if (helmet) helmet.remove()
  const script = doc.querySelector('script[data-dc-script]').textContent
  const Component = new Function('DCLogic', script + '\nreturn Component;')(DCLogic)
  return { doc, root: xdc, style, Component }
}

function lookup(expr, scope) {
  const e = expr.trim()
  if (e === 'true') return true
  if (e === 'false') return false
  if (/^-?\d+(\.\d+)?$/.test(e)) return Number(e)
  return e.split('.').reduce((v, k) => (v == null ? undefined : v[k]), scope)
}

const hole = /\{\{\s*([^}]+?)\s*\}\}/g
const interp = (s, scope) => s.replace(hole, (_, e) => { const v = lookup(e, scope); return v == null ? '' : String(v) })
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const escAttr = (s) => esc(s).replace(/"/g, '&quot;')
const voids = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'source', 'track', 'wbr'])

function render(node, scope, styles) {
  if (node.nodeType === 3) return esc(interp(node.textContent, scope))
  if (node.nodeType !== 1) return ''
  const tag = node.tagName.toLowerCase()
  const kids = () => [...node.childNodes].map((c) => render(c, scope, styles)).join('')
  if (tag === 'sc-if') return lookup(node.getAttribute('value').replace(/[{}]/g, ''), scope) ? kids() : ''
  if (tag === 'sc-for') {
    const list = lookup(node.getAttribute('list').replace(/[{}]/g, ''), scope) || []
    const as = node.getAttribute('as')
    return list.map((item, i) => [...node.childNodes].map((c) => render(c, { ...scope, [as]: item, $index: i }, styles)).join('')).join('')
  }
  if (tag === 'dc-import') {
    const props = {}
    for (const a of node.attributes) {
      if (a.name === 'name' || a.name.startsWith('hint-')) continue
      const whole = a.value.match(/^\{\{\s*([^}]+?)\s*\}\}$/)
      props[a.name.replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = whole ? lookup(whole[1], scope) : a.value
    }
    return renderBoard(node.getAttribute('name'), props, styles)
  }
  let attrs = ''
  for (const a of node.attributes) {
    if (/^on[A-Z]/.test(a.name) || /^on[a-z]+$/.test(a.name) || a.name.startsWith('hint-')) continue
    attrs += ` ${a.name}="${escAttr(interp(a.value, scope))}"`
  }
  if (voids.has(tag)) return `<${tag}${attrs}>`
  return `<${tag}${attrs}>${kids()}</${tag}>`
}

function renderBoard(name, props, styles) {
  const b = load(name)
  if (b.style && !styles.includes(b.style)) styles.push(b.style)
  const vals = new b.Component(props).renderVals()
  return [...b.root.childNodes].map((c) => render(c, vals, styles)).join('')
}

const head = (title, style) => `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&amp;family=IBM+Plex+Sans+Condensed:wght@500;600;700&amp;family=IBM+Plex+Sans:wght@400;500;600;700&amp;family=Pixelify+Sans:wght@400;500;600;700&amp;display=swap">
<link rel="stylesheet" href="../tokens.css">
<link rel="stylesheet" href="../components.css">
<link rel="stylesheet" href="../library.css">
<link rel="stylesheet" href="../tips.css">
<style>
${style}
</style>
</head>
<body>
`
const tail = `
<!-- Add ?theme=day to the address to see the Day theme. -->
<script>if (new URLSearchParams(location.search).get("theme") === "day") document.querySelectorAll(".rs").forEach((e) => e.classList.add("day"));</script>
</body>
</html>
`

const screens = [
  ['MS-desk-library-album-history', 'Lib-HistDesk', {}, 'Desktop · album History, one Save open', 'desk'],
  ['MS-desk-library-album-history-compact', 'Lib-HistDesk', { density: 'compact', tip: false }, 'Desktop · album History, Compact rows', 'desk'],
  ['MS-desk-library-album-history-day', 'Lib-HistDesk', { theme: 'day', tip: false }, 'Desktop · album History, Day', 'desk'],
  ['MS-desk-library-history-cap', 'Lib-HistDesk', { view: 'editor' }, 'Desktop · the Editor with the History cap', 'desk'],
  ['MS-phone-library-history-cap', 'Lib-HistPhone', { screen: 'album' }, 'Phone · album with the History cap', 'phone'],
  ['MS-phone-library-history', 'Lib-HistPhone', { screen: 'history' }, 'Phone · album History', 'phone'],
  ['MS-phone-library-history-open', 'Lib-HistPhone', { screen: 'open' }, 'Phone · album History, one Save open', 'phone'],
  ['MS-phone-library-history-run', 'Lib-HistPhone', { screen: 'run' }, 'Phone · album History, a run open', 'phone'],
  ['MS-library-history-states', 'Lib-HistStates', {}, 'Music Shelf · album History states', 'sheet'],
]

for (const [file, board, props, title, kind] of screens) {
  const styles = []
  let body = renderBoard(board, props, styles)
  if (kind === 'desk') {
    body = body.replace('width: 1440px; height: 900px;', 'height: 100vh; min-height: 640px;')
    body = `<div style="min-height: 100vh; background: #14171C">\n${body}\n</div>`
  } else if (kind === 'phone') {
    body = `<div style="width: 390px; height: 844px">\n${body}\n</div>`
  }
  const full = title.startsWith('Music Shelf') ? title : `Music Shelf · ${title}`
  fs.writeFileSync(path.join(outDir, file + '.html'), head(full, styles.join('\n')) + body + tail)
  console.log(file)
}
