const svgNamespace = 'http://www.w3.org/2000/svg'
const materialSelector = [
  '.liquid-selection-pill', '.liquid-switch-thumb', '.liquid-layout .sidebar',
  '.liquid-layout header.glass', '.liquid-public > header', '.settings-tabs-shell',
  '.btn-secondary', '.select-trigger', '.date-picker-trigger', '.liquid-control',
  '.dashboard-actions button', '.liquid-popover', '.dropdown',
  '.select-dropdown-liquid', '.date-picker-dropdown-liquid', '.liquid-toast'
].join(', ')

/** A flat center and a rounded bevel, sampled with Snell's law (glass IOR 1.45).
 * R/G encode the backdrop sample offset; this never distorts the foreground label.
 */
export function liquidDisplacement(width: number, height: number, corner: number) {
  const ratio = Math.min(1, 768 / Math.max(width, height))
  const mapWidth = Math.max(1, Math.round(width * ratio))
  const mapHeight = Math.max(1, Math.round(height * ratio))
  const data = new Uint8ClampedArray(mapWidth * mapHeight * 4)
  const radius = Math.max(1, Math.min(corner, width / 2, height / 2))
  const bevel = Math.max(1, Math.min(16, radius * 0.8, height * 0.3))
  const depth = Math.min(12, height * 0.22)
  const scale = depth * 2 + 2

  for (let y = 0; y < mapHeight; y++) {
    for (let x = 0; x < mapWidth; x++) {
      const px = (x + 0.5) / mapWidth * width - width / 2
      const py = (y + 0.5) / mapHeight * height - height / 2
      const qx = Math.abs(px) - (width / 2 - radius)
      const qy = Math.abs(py) - (height / 2 - radius)
      const ox = Math.max(qx, 0)
      const oy = Math.max(qy, 0)
      const length = Math.hypot(ox, oy)
      const inside = radius - length - Math.min(Math.max(qx, qy), 0)
      let dx = 0
      let dy = 0
      if (inside >= 0 && inside < bevel) {
        const nx = length > 0 ? ox / length : qx > qy ? 1 : 0
        const ny = length > 0 ? oy / length : qx > qy ? 0 : 1
        const t = inside / bevel
        const incident = Math.acos(t)
        const refracted = Math.asin(Math.sin(incident) / 1.45)
        const offset = Math.tan(incident - refracted) * depth * (1 - t) ** 0.35
        dx = -Math.sign(px) * nx * offset
        dy = -Math.sign(py) * ny * offset
      }
      const index = (y * mapWidth + x) * 4
      data[index] = Math.round(255 * (0.5 + dx / scale))
      data[index + 1] = Math.round(255 * (0.5 + dy / scale))
      data[index + 2] = 128
      data[index + 3] = 255
    }
  }
  return { data, width: mapWidth, height: mapHeight, scale }
}

/** Each optical surface gets a map fitted to its own actual rounded outline.
 * Maps are shared by equal sizes; detached menus/route controls release their filters.
 */
export function createLiquidRefraction(root: HTMLElement, definitions: SVGDefsElement, prefix: string) {
  if (typeof CSS === 'undefined' || !CSS.supports?.('backdrop-filter', 'url("#liquid-lens")')) return () => {}
  const canvas = document.createElement('canvas')
  const context = canvas.getContext('2d')
  if (!context) return () => {}
  const painter: CanvasRenderingContext2D = context

  type Surface = { filter: SVGFilterElement; image: SVGFEImageElement; displacement: SVGFEDisplacementMapElement; key: string }
  const surfaces = new Map<HTMLElement, Surface>()
  const cache = new Map<string, { url: string; scale: number }>()
  const dirty = new Set<HTMLElement>()
  let serial = 0
  let frame = 0
  let discover = false
  let alive = true

  function remove(element: HTMLElement, surface: Surface) {
    resize.unobserve(element)
    element.style.removeProperty('--liquid-refraction')
    surface.filter.remove()
    surfaces.delete(element)
    dirty.delete(element)
  }

  function add(element: HTMLElement) {
    if (surfaces.has(element) || definitions.contains(element)) return
    const filter = document.createElementNS(svgNamespace, 'filter')
    const id = `${prefix}-surface-${++serial}`
    filter.setAttribute('id', id)
    filter.setAttribute('x', '0')
    filter.setAttribute('y', '0')
    filter.setAttribute('width', '100%')
    filter.setAttribute('height', '100%')
    filter.setAttribute('color-interpolation-filters', 'sRGB')
    const image = document.createElementNS(svgNamespace, 'feImage')
    image.setAttribute('x', '0')
    image.setAttribute('y', '0')
    image.setAttribute('preserveAspectRatio', 'none')
    image.setAttribute('result', 'lens')
    const displacement = document.createElementNS(svgNamespace, 'feDisplacementMap')
    displacement.setAttribute('in', 'SourceGraphic')
    displacement.setAttribute('in2', 'lens')
    displacement.setAttribute('xChannelSelector', 'R')
    displacement.setAttribute('yChannelSelector', 'G')
    filter.append(image, displacement)
    definitions.appendChild(filter)
    surfaces.set(element, { filter, image, displacement, key: '' })
    resize.observe(element)
    dirty.add(element)
  }

  function render() {
    frame = 0
    if (!alive) return
    if (discover) {
      discover = false
      for (const [element, surface] of surfaces) if (!root.contains(element)) remove(element, surface)
      root.querySelectorAll<HTMLElement>(materialSelector).forEach(add)
    }
    for (const element of dirty) {
      const surface = surfaces.get(element)
      if (!surface || !element.isConnected) continue
      const rect = element.getBoundingClientRect()
      if (rect.width < 1 || rect.height < 1) continue
      const width = Math.max(2, Math.round(rect.width / 2) * 2)
      const height = Math.max(2, Math.round(rect.height / 2) * 2)
      const cornerStyle = getComputedStyle(element).borderTopLeftRadius
      const cornerValue = parseFloat(cornerStyle) || 0
      const corner = Math.min(cornerStyle.endsWith('%') ? Math.min(width, height) * cornerValue / 100 : cornerValue, width / 2, height / 2)
      const key = `${width}:${height}:${Math.round(corner)}`
      if (surface.key === key) continue
      let map = cache.get(key)
      if (!map) {
        const pixels = liquidDisplacement(width, height, corner)
        canvas.width = pixels.width
        canvas.height = pixels.height
        const image = painter.createImageData(pixels.width, pixels.height)
        image.data.set(pixels.data)
        painter.putImageData(image, 0, 0)
        map = { url: canvas.toDataURL(), scale: pixels.scale }
        if (cache.size >= 64) cache.delete(cache.keys().next().value!)
        cache.set(key, map)
      }
      surface.image.setAttribute('href', map.url)
      // Percentages resolve against the hidden SVG viewport (0 × 0), not the
      // filtered HTML control. Explicit user-space dimensions keep the map valid.
      surface.image.setAttribute('width', String(width))
      surface.image.setAttribute('height', String(height))
      surface.displacement.setAttribute('scale', String(map.scale))
      surface.key = key
      element.style.setProperty('--liquid-refraction', `url(#${surface.filter.id})`)
    }
    dirty.clear()
  }

  function schedule() { if (alive && !frame) frame = requestAnimationFrame(render) }
  const resize = new ResizeObserver(entries => {
    entries.forEach(entry => dirty.add(entry.target as HTMLElement))
    schedule()
  })
  const mutations = new MutationObserver(records => {
    if (records.every(record => definitions.contains(record.target))) return
    discover = true
    schedule()
  })
  mutations.observe(root, { childList: true, subtree: true })
  discover = true
  schedule()

  return () => {
    alive = false
    cancelAnimationFrame(frame)
    mutations.disconnect()
    for (const [element, surface] of surfaces) remove(element, surface)
    resize.disconnect()
    cache.clear()
  }
}
