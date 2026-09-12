import { useEffect, useRef } from 'react'

// Renders the configured captcha widget and reports the token. Scripts are
// loaded once per provider; each provider exposes an explicit render call.
declare global {
  interface Window {
    turnstile?: { render: (el: HTMLElement, opts: { sitekey: string; callback: (t: string) => void; 'expired-callback'?: () => void; theme?: string }) => string }
    grecaptcha?: { render: (el: HTMLElement, opts: { sitekey: string; callback: (t: string) => void; 'expired-callback'?: () => void }) => number; ready?: (cb: () => void) => void }
    hcaptcha?: { render: (el: HTMLElement, opts: { sitekey: string; callback: (t: string) => void; 'expired-callback'?: () => void }) => string }
  }
}

const scripts: Record<string, string> = {
  turnstile: 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit',
  recaptcha: 'https://www.google.com/recaptcha/api.js?render=explicit',
  hcaptcha: 'https://js.hcaptcha.com/1/api.js?render=explicit',
}

function load(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if (document.querySelector(`script[src="${src}"]`)) { resolve(); return }
    const s = document.createElement('script')
    s.src = src; s.async = true; s.defer = true
    s.onload = () => resolve(); s.onerror = () => reject(new Error('captcha script failed to load'))
    document.head.appendChild(s)
  })
}

export function Captcha({ provider, siteKey, onToken }: { provider: string; siteKey: string; onToken: (t: string) => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const rendered = useRef(false)
  useEffect(() => {
    if (!ref.current || rendered.current || !scripts[provider]) return
    rendered.current = true
    const el = ref.current
    load(scripts[provider]).then(() => {
      const poll = () => {
        const api = provider === 'turnstile' ? window.turnstile : provider === 'recaptcha' ? window.grecaptcha : window.hcaptcha
        if (!api) { setTimeout(poll, 100); return }
        const opts = { sitekey: siteKey, callback: onToken, 'expired-callback': () => onToken('') }
        if (provider === 'recaptcha' && window.grecaptcha?.ready) window.grecaptcha.ready(() => window.grecaptcha!.render(el, opts))
        else api.render(el, opts as never)
      }
      poll()
    }).catch(() => {})
  }, [provider, siteKey, onToken])
  return <div ref={ref} />
}
