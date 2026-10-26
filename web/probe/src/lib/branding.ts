import { applyBrandMetadata, brandLogo } from '../../../shared/branding.ts'
import type { Snapshot } from './api'

// Inheritance is resolved by /api/probe. An independent probe must never fill
// missing brand fields from /api/site, including on its dedicated hostname.
type ProbeBrand = Pick<Snapshot, 'title' | 'logo' | 'logo_dark' | 'page_title' | 'description' | 'favicon'>

export function probeLogo(snapshot: ProbeBrand | null | undefined, scheme: 'light' | 'dark'): string {
  return brandLogo({ logo_light: snapshot?.logo, logo_dark: snapshot?.logo_dark }, scheme)
}

export function applyProbeMetadata(snapshot: Pick<ProbeBrand, 'title' | 'page_title' | 'description' | 'favicon'>): () => void {
  return applyBrandMetadata({ name: snapshot.title, title: snapshot.page_title, description: snapshot.description, favicon: snapshot.favicon })
}
