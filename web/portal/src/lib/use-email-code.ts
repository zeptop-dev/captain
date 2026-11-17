import { useEffect, useState } from 'react'
import { ApiError } from './api'

// The cooldown belongs to the address actually sent, never a later field value.
export function useEmailCode(email: string) {
  const [sending, setSending] = useState(false)
  const [delivery, setDelivery] = useState({ email: '', deadline: 0, sent: false })
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 500)
    return () => window.clearInterval(timer)
  }, [])
  const target = email.trim().toLowerCase()
  const seconds = delivery.email === target ? Math.max(0, Math.ceil((delivery.deadline - now) / 1000)) : 0
  async function send(request: () => Promise<{ retry_after?: number }>) {
    setSending(true)
    try {
      const result = await request()
      setDelivery({ email: target, deadline: Date.now() + (result.retry_after ?? 60) * 1000, sent: true })
    } catch (error) {
      if (error instanceof ApiError && error.retryAfter) setDelivery({ email: target, deadline: Date.now() + error.retryAfter * 1000, sent: false })
      throw error
    } finally { setNow(Date.now()); setSending(false) }
  }
  return { sending, seconds, sent: delivery.email === target && delivery.sent, send }
}
