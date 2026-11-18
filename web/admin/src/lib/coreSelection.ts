import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from './api'
import type { Readiness } from '../components/PrivateAccessReadiness'

type CoreOption = { name: string; compatible: boolean; reason?: string; available: boolean | null; private_access_policy?: { scoped: boolean; independent_dns: boolean } }
type CoreOptions = { options: CoreOption[]; inventory_known: boolean; auto_core: string; private_access_readiness?: Readiness }
const coreNames: Record<string, string> = { 'singbox-extended': 'sing-box Extended', singbox: 'sing-box', xray: 'Xray', mita: 'mita (Mieru)', hysteria: 'Hysteria 2', snell: 'snell-server' }
export const coreName = (name: string) => coreNames[name] || name
const object = (v: unknown): Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : {}

// Send only non-secret capability features. Inbound JSON contains passwords and
// private keys, which must never enter URLs or TanStack Query's cache keys.
function probe(ib: Record<string, unknown>): string {
  return new URLSearchParams({
    standalone: String(!!ib.standalone),
    wireguard_transit: String(!!ib.wireguard_slot),
    bound_listen: String(!!ib.listen && !['::', '0.0.0.0'].includes(String(ib.listen))),
    private_enforcement: String(object(ib.private_access).enforcement || 'dual'),
    allow_core_only: String(object(ib.private_access).allow_core_only === true),
    private_access: String(!!ib.private_access && object(ib.private_access).mode !== 'off'),
    reverse: String(!!ib.reverse), protocol: String(ib.protocol || ''), transport: String(object(ib.transport).type || 'tcp'), cipher: String(ib.cipher || ''),
    reality: String(object(ib.tls).mode === 2), shadow_tls: String(!!ib.shadow_tls), fallbacks: String(Array.isArray(ib.fallbacks) && ib.fallbacks.length > 0),
    proxy_protocol: String(ib.accept_proxy_protocol === true), snell_multi_user: String(ib.snell_multi_user === true), snell_obfs: String(ib.snell_obfs || ''),
  }).toString()
}

export function useCoreSelection(endpoint: string, inbound: Record<string, unknown> | undefined, selected: string) {
  const { t } = useTranslation()
  const queryString = inbound ? probe(inbound) : ''
  const q = useQuery({ queryKey: ['core-options', endpoint, queryString], queryFn: () => api.get<CoreOptions>(`${endpoint}?${queryString}`), enabled: !!inbound && !!endpoint, staleTime: 0, refetchInterval: 15_000 })
  const result = q.data
  const chosen = result?.options.find((c) => c.name === selected)
  const incompatible = !!selected && !!result && (!chosen || !chosen.compatible)
  const unavailable = chosen?.available === false
  const noAuto = !selected && result?.inventory_known && !result.auto_core
  const privateUnknown = !!inbound?.private_access && object(inbound.private_access).mode !== 'off' && !result?.inventory_known
  const extensionUnknown = (!!inbound?.standalone || selected === 'singbox-extended' || inbound?.protocol === 'ssh') && !result?.inventory_known
  const runtimeMode = result?.private_access_readiness?.effective_mode
  const coreOnly = runtimeMode === 'core_only' || object(inbound?.private_access).enforcement === 'core_only'
  const reason = chosen?.reason || result?.options.find(c => c.available && c.reason === 'privateAccessConfirmation')?.reason || result?.options.find(c => c.available)?.reason
  const failure = result?.private_access_readiness?.checks.find(c => coreOnly && c.id === 'guard' ? c.ok : !c.ok)
  const privateFailure = inbound?.private_access && object(inbound.private_access).mode !== 'off' && failure && (noAuto || chosen?.reason === 'privateAccess')
  const error = (incompatible || noAuto) && reason?.startsWith('privateAccess') ? t(`inbounds.coreSelection.reasons.${reason}`) : privateFailure ? `${t(`privateReady.checks.${failure.id}`)}: ${t(`privateReady.fixes.${failure.id}`)}` : extensionUnknown ? t(inbound?.standalone ? 'inbounds.coreSelection.reasons.standaloneAuth' : 'coreManager.unavailable') : privateUnknown ? t('privateReady.unknown') : incompatible ? t(`inbounds.coreSelection.reasons.${chosen?.reason || 'unknown'}`) : unavailable ? t('inbounds.coreSelection.unavailable') : noAuto ? t('inbounds.coreSelection.noAvailable') : undefined
  const data = [
    { value: '', label: t('inbounds.coreSelection.auto') },
    ...(result?.options ?? []).filter((c) => c.compatible || c.name === selected).map((c) => ({ value: c.name, label: `${coreName(c.name)}${c.available === false ? ` · ${t('inbounds.coreSelection.notEnabled')}` : ''}`, disabled: !c.compatible || c.available === false })),
  ]
  if (selected && !data.some((c) => c.value === selected)) data.push({ value: selected, label: coreName(selected), disabled: true })
  const description = !inbound ? t('inbounds.coreSelection.invalidJSON') : q.isError ? t('inbounds.coreSelection.loadFailed') : !result ? t('inbounds.coreSelection.loading') : !result.inventory_known ? t('inbounds.coreSelection.unknown') : !selected && result.auto_core ? t('inbounds.coreSelection.preview', { core: coreName(result.auto_core) }) : t('inbounds.coreSelection.enabledHint')
  return {
    runtimeMode,
    selectProps: { data, error, description, inputWrapperOrder: ['label', 'input', 'description', 'error'] as ('label' | 'input' | 'description' | 'error')[] },
    blocked: !!error || !inbound || !result || q.isError,
  }
}
