import { useContext, useState } from 'react'
import { brandLogo, type Branding } from '../../../shared/branding'
import { SiteSchemeContext } from './SiteThemeSelector'

export function SiteBrandMark({ branding }: { branding?: Branding }) {
  const { selected } = useContext(SiteSchemeContext)
  const src = brandLogo(branding, selected)
  const [failed, setFailed] = useState('')
  if (src && failed !== src) return <img className="site-brand-logo" src={src} alt="" aria-hidden="true" onError={() => setFailed(src)} />
  return <svg viewBox="0 0 28 28" width="25" height="25" fill="none" aria-hidden="true"><path d="m14 2 10.4 6v12L14 26 3.6 20V8Z" stroke="currentColor" strokeWidth="1.3" /><path d="m14 7 6 14-6-4-6 4Z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /><path d="M14 7v10" stroke="currentColor" strokeWidth="1.3" /></svg>
}
