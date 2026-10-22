// Only the login authorization page can be resumed after authentication.
export function loginReturn(search: string): string {
  const next = new URLSearchParams(search).get('next') ?? ''
  if (next === '/portal/authorize' || next.startsWith('/portal/authorize?')) return next
  return '/portal/'
}
