export const referralCode = (search: string): string => new URLSearchParams(search).get('ref')?.trim() ?? ''

export function withReferral(path: string, code: string): string {
  if (!code.trim()) return path
  const [pathname, search] = path.split('?')
  const params = new URLSearchParams(search)
  params.set('ref', code.trim())
  return pathname + '?' + params.toString()
}

// Only callers' known portal paths and loginReturn's allowlisted next are used.
export const authDestination = (path: string, next: string, code: string): string => withReferral(path + '?' + new URLSearchParams({ next }).toString(), code)

// Bootstrap attribution and a later edited invite must settle in order. OAuth
// waits for this same promise, so it cannot race the HttpOnly referral cookie.
export function createReferralWriter(write: (code: string) => Promise<void>) {
  let latest: { code: string; promise: Promise<void> } | undefined
  return (value: string): Promise<void> => {
    const code = value.trim()
    // Empty explicit invites retain the server's existing cookie fallback.
    if (!code) return latest?.promise ?? Promise.resolve()
    if (latest?.code === code) return latest.promise
    const previous = latest?.promise
    const promise = (previous ?? Promise.resolve()).catch(() => {}).then(() => write(code))
    const current = { code, promise }
    latest = current
    void promise.catch(() => { if (latest === current) latest = undefined })
    return promise
  }
}

export const rememberReferral = createReferralWriter(async code => {
  const response = await fetch('/api/portal/ref', {
    method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ Code: code }), signal: AbortSignal.timeout(10_000),
  })
  if (!response.ok) throw new Error('Could not save invitation')
})
