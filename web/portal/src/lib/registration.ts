export interface RegistrationLegal { terms_url: string; privacy_url: string; document_version: string }
export interface RegistrationPolicy {
  open: boolean; verify: boolean; reset: boolean; invite_only: boolean; email_suffixes: string[]
  captcha?: { provider: string; site_key: string }; legal?: RegistrationLegal | null; legal_version?: string
}

// Codes are additive; older servers still have a narrow, known-message fallback.
export function registrationErrorKey(code: string | undefined, message: string): string | undefined {
  const keys: Record<string, string> = {
    registration_closed: 'closed', email_domain: 'domainDenied', invite_required: 'inviteDenied',
    registration_ip_limit: 'ipLimited', captcha_failed: 'captchaFailed', legal_changed: 'legalChanged',
    code_invalid: 'codeInvalid', code_wait: 'codeWait', email_in_use: 'emailInUse',
    mail_failed: 'mailFailed', mail_unavailable: 'mailUnavailable', code_rate_limit: 'codeRateLimit',
  }
  if (code && keys[code]) return `auth.${keys[code]}`
  if (message === 'registration is closed') return 'auth.closed'
  if (message === 'email already registered') return 'auth.emailInUse'
  if (message === 'this email domain is not accepted') return 'auth.domainDenied'
  return undefined
}
export function registrationErrorField(code: string | undefined): string | undefined {
  if (code === 'email_domain' || code === 'email_in_use') return 'Email'
  if (code === 'invite_required') return 'Invite'
  if (code === 'code_invalid') return 'Code'
  return undefined
}
