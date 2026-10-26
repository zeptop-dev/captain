// Layout from vlongx/komari-theme-Glassmorphism-three-network (MIT).
import { t } from '@shared/i18n'
import { DataTooltip } from '@/components/ui/data-tooltip'
import { useNodeCarrierPingDisplay } from '@/hooks/useNodeCarrierPingDisplay'
import { vclass } from '@/lib/vclass'
import { useApp } from '@/stores/app'

export default function ThreeNetworkPing({ uuid, nodeName, online, enabled, onClick }: { uuid: string, nodeName: string, online: boolean, enabled: boolean, onClick?: () => void }) {
  const nodeCardSize = useApp(s => s.nodeCardSize)
  const { carrierDisplays, dual } = useNodeCarrierPingDisplay(uuid, enabled)
  const panelClass = dual ? (nodeCardSize === 'mini' ? 'h-[164px] gap-1 p-1' : 'h-[184px] gap-1.5 p-2') : nodeCardSize === 'large' ? 'h-[128px] gap-2 p-2' : nodeCardSize === 'comfortable' ? 'h-[120px] gap-1.5 p-2' : nodeCardSize === 'mini' ? 'h-[92px] gap-1 p-1' : 'h-[112px] gap-1.5 p-1.5'

  const panel = (metric: 'latency' | 'loss') => (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onClick?.()
      }}
      aria-label={`${nodeName} ${t('网络监测')}`}
      className={vclass('group/panel relative flex flex-col rounded-lg bg-slate-500/5', [panelClass, !online ? 'blur-xs opacity-50' : ''])}
    >
      <div className="flex items-center justify-between text-[11px] leading-none">
        <span className="text-muted-foreground">{t(metric === 'latency' ? '延迟' : '丢包')}</span>
        <span className="text-[10px] text-muted-foreground/70">{t('三网')}</span>
      </div>

      <div className="grid min-h-0 flex-1 grid-rows-3 gap-1">
        {carrierDisplays.map((carrier) => {
          return (
            <div
              key={`${carrier.key}-${metric}`}
              data-carrier={carrier.key}
              data-carrier-metric={metric}
              className="flex min-h-0 flex-col gap-[2px]"
              title={metric === 'latency' ? carrier.latencyTooltip : carrier.lossTooltip}
            >
              <div className={vclass('flex text-[10px] leading-none', dual ? 'flex-col items-stretch gap-[3px]' : 'items-center justify-between')}>
                <span className="flex min-w-0 items-center gap-1 text-muted-foreground">
                  <span className={vclass('size-1.5 shrink-0 rounded-full', carrier.dotClass)} />
                  <span className="truncate">{carrier.label}</span>
                </span>
                <span className="shrink-0 tabular-nums font-medium">{metric === 'latency' ? carrier.latencyDisplay : carrier.lossDisplay}</span>
              </div>
              {carrier.series.map(series => {
                const bars = metric === 'latency' ? series.latencyBars : series.lossBars
                return <div key={series.family ?? 'legacy'} data-carrier-family={series.family} className={dual ? 'flex items-center gap-1' : ''}>
                  {dual && <span aria-hidden="true" className="w-3 shrink-0 text-[8px] leading-none text-muted-foreground">{series.familyLabel}</span>}
                  <div
                    className="grid h-1.5 min-w-0 flex-1 items-end gap-[1px] opacity-80 group-hover/panel:opacity-100"
                    style={{ gridTemplateColumns: `repeat(${bars.length}, minmax(0, 1fr))` }}
                  >
                    {bars.map(bar => (
                      <DataTooltip key={bar.key} placement="top" content={bar.tooltip} className="h-full w-full">
                        <span className={vclass('block h-full w-full rounded-[1px] transition-transform duration-150 group-hover/data-tooltip:scale-y-160 group-hover/panel:opacity-60 group-hover/data-tooltip:!opacity-100', bar.className)} />
                      </DataTooltip>
                    ))}
                  </div>
                </div>
              })}
            </div>
          )
        })}
      </div>
    </button>
  )

  return (
    <div className="grid grid-cols-2 gap-1.5">
      {panel('latency')}
      {panel('loss')}
    </div>
  )
}
