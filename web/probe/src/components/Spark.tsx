// Tiny canvas sparkline: no chart library on the list page, one <canvas>
// per node, redrawn when the data changes.
import { useEffect, useRef } from 'react'

export function Spark({ values, color = '#22d3ee', height = 32, max }: { values: number[]; color?: string; height?: number; max?: number }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    const c = ref.current
    if (!c) return
    const dpr = window.devicePixelRatio || 1
    const w = c.clientWidth || 160
    c.width = w * dpr; c.height = height * dpr
    const ctx = c.getContext('2d')!
    ctx.scale(dpr, dpr)
    ctx.clearRect(0, 0, w, height)
    if (values.length < 2) return
    const m = max ?? Math.max(1, ...values)
    const step = w / (values.length - 1)
    ctx.beginPath()
    values.forEach((v, i) => { const y = height - (Math.min(v, m) / m) * (height - 2) - 1; if (i === 0) ctx.moveTo(0, y); else ctx.lineTo(i * step, y) })
    ctx.strokeStyle = color; ctx.lineWidth = 1.5; ctx.stroke()
    ctx.lineTo(w, height); ctx.lineTo(0, height); ctx.closePath()
    ctx.fillStyle = color + '22'; ctx.fill()
  }, [values, color, height, max])
  return <canvas ref={ref} style={{ width: '100%', height }} />
}
