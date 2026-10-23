import { useState } from 'react'
import { Icon } from '@iconify/react'
import { t } from '@shared/i18n'
import { captainStorage, shows, snapshot } from '@shared/captain'
import { storageCapacity, storageFullCapacity, storagePercent, type StorageItem } from '../../../src/lib/storage'
import { AppDialog } from '@/components/ui/app-dialog'
import { ProgressThin } from '@/components/ui/progress-thin'
import { useBox } from '@/hooks/useBox'

const compactPercent = (value: number | null) => value === null ? '—' : `${Math.round(value)}%`
const shortLabel = (item: StorageItem, items: StorageItem[]) => item.name?.trim() ? item.label : item.kind !== 'system' && items.filter(v => v.kind === item.kind).length === 1 ? t(`storage.${item.kind}`) : item.label.replace(/ ([0-9]+)$/, '$1')
const status = (percent: number | null) => percent === null ? 'default' : percent >= 90 ? 'error' : percent >= 70 ? 'warning' : 'success'

export function StorageVolumes({ items }: { items: StorageItem[] }) {
  return items.length ? <div className="grid grid-cols-1 gap-3 sm:grid-cols-2" data-storage-volumes>
    {items.map(item => <div key={item.id} className="min-w-0 rounded-md bg-slate-500/5 p-3" data-storage-volume={item.id}>
      <div className="flex items-start justify-between gap-3 text-xs">
        <span className="min-w-0 break-words font-medium">{item.label}</span>
        <span className="shrink-0 tabular-nums">{storagePercent(item.percent)}</span>
      </div>
      <ProgressThin percentage={item.percent ?? 0} status={status(item.percent)} height={4} className="my-2" />
      <div className="text-xs text-muted-foreground tabular-nums">{storageFullCapacity(item)}</div>
    </div>)}
  </div> : <div className="text-xs text-muted-foreground" data-storage-empty>{t('storage.empty')}</div>
}

/** Keeps the existing card height; the whole storage cell is a touch target. */
export default function StorageSummary({ uuid, list = false }: { uuid: string; list?: boolean }) {
  useBox(snapshot)
  const [open, setOpen] = useState(false)
  const items = captainStorage(uuid, t)
  if (!shows('disk')) return null
  const visible = items.slice(0, list ? 1 : 2)
  const multi = visible.length > 1
  const more = items.length - visible.length
  return <div className="min-w-0">
    <button type="button" data-storage-summary data-storage-open className={`block w-full min-w-0 text-left rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring ${list ? '' : 'h-[44.5px]'}`}
      aria-label={t('storage.viewAll')} onClick={event => { event.stopPropagation(); setOpen(true) }} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') event.stopPropagation() }}>
      {list ? <div className="space-y-1">
        <div className="flex min-w-0 justify-between gap-1 text-[11px] text-foreground/75"><span className="truncate">{visible[0]?.label ?? t('storage.title')}</span><span className="shrink-0 tabular-nums">{storagePercent(visible[0]?.percent ?? null)}</span></div>
        <ProgressThin percentage={visible[0]?.percent ?? 0} status={status(visible[0]?.percent ?? null)} height={4} />
        {more > 0 && <div className="text-[9px] leading-[10px] text-muted-foreground">{t('storage.more').replace('{count}', String(more))}</div>}
      </div> : items.length === 0 ? <div className="flex h-full flex-col justify-between text-xs text-muted-foreground"><span>{t('storage.title')}</span><span>—</span></div> : <>
        <span className={`grid ${multi ? 'grid-cols-2 gap-x-2' : 'grid-cols-1'}`}>
          {visible.map(item => <span key={item.id} data-storage-volume={item.id} className={`flex min-w-0 flex-col ${multi ? 'gap-0.5' : 'gap-1'}`} title={`${item.label}: ${storageCapacity(item)}`}>
            <span className={`flex min-w-0 justify-between gap-0.5 ${multi ? 'text-[10px] leading-3' : 'text-xs'}`}>
              <span className="inline-flex min-w-0 items-center gap-1 text-muted-foreground">
                {!multi && <Icon icon="tabler:server-2" data-node-metric-icon="disk" width="13" height="13" className="shrink-0 text-orange-500" aria-hidden="true" />}
                <span className="truncate">{multi ? shortLabel(item, items) : item.label}</span>
              </span>
              <span className="shrink-0 tabular-nums font-medium">{multi ? compactPercent(item.percent) : storagePercent(item.percent)}</span>
            </span>
            <ProgressThin percentage={item.percent ?? 0} status={status(item.percent)} height={4} />
            <span data-storage-capacity className={`block truncate text-muted-foreground tabular-nums ${multi ? 'text-[9px] leading-[14px] tracking-tight' : 'text-[11px]'}`}>{multi ? storageCapacity(item, true) : storageCapacity(item)}</span>
          </span>)}
        </span>
        {more > 0 && <span className="block text-right text-[9px] leading-[10px] text-muted-foreground">{t('storage.more').replace('{count}', String(more))}</span>}
      </>}
    </button>
    <div onClick={event => event.stopPropagation()} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') event.stopPropagation() }}>
      <AppDialog open={open} title={t('storage.title')} description={t('storage.hint')} contentClass="max-w-lg" onOpenChange={setOpen}>
        <StorageVolumes items={items} />
      </AppDialog>
    </div>
  </div>
}
