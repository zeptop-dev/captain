import { useMemo } from 'react'
import { snapshot } from '@shared/captain'
import { t } from '@shared/i18n'
import { selectPings } from '@shared/ping-selection'
import { useBox } from '@/hooks/useBox'
import { useNodeCarrierPingDisplay } from '@/hooks/useNodeCarrierPingDisplay'
import { useNodePingDisplay } from '@/hooks/useNodePingDisplay'
import { vclass } from '@/lib/vclass'

export default function NodePingListCell({ uuid, online: _online, enabled, onClick }: { uuid: string, online: boolean, enabled: boolean, onClick?: () => void }) {
  const snap = useBox(snapshot)
  const node = useMemo(() => snap?.nodes.find(n => String(n.id) === uuid), [snap, uuid])
  const threeNetwork = selectPings(node, snap?.carrier_ping ?? false).threeNetwork
  const { carrierDisplays, dual } = useNodeCarrierPingDisplay(uuid, enabled && threeNetwork)
  const { latencyRenderBars, lossRenderBars } = useNodePingDisplay(uuid, { enabled: enabled && !threeNetwork })

  const click = (e: { stopPropagation: () => void }) => {
    e.stopPropagation()
    onClick?.()
  }
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation()

  // Keep the upstream 64px list row: card panels are too tall for this cell.
  if (threeNetwork) {
    return (
      <button
        type="button"
        className={vclass('group flex w-full min-w-0 flex-col pr-4 text-left', dual ? 'gap-px' : 'gap-[2px]')}
        aria-label={`${node?.name ?? ''} ${t('网络监测')}`}
        onClick={click}
        onKeyDown={stop}
      >
        {carrierDisplays.map(carrier => (
          <span
            key={carrier.key}
            data-carrier={carrier.key}
            className="flex min-w-0 items-center gap-1"
            title={`${carrier.label} · ${t('延迟')} ${carrier.latencyDisplay} · ${t('丢包')} ${carrier.lossDisplay}`}
          >
            <span className={vclass('size-1.5 shrink-0 rounded-full', carrier.dotClass)} />
            <span className="flex min-w-0 flex-1 flex-col gap-[1px]">
              {carrier.series.map(series => <span key={series.family ?? 'legacy'} data-carrier-family={series.family} className={dual ? 'flex min-w-0 items-center gap-1' : 'contents'}>
                {dual && <span className="w-3 shrink-0 text-[6px] leading-none text-muted-foreground">{series.familyLabel}</span>}
                <span className={dual ? 'flex min-w-0 flex-1 flex-col gap-px' : 'contents'}>
                  {(['latency', 'loss'] as const).map(metric => {
                    const bars = metric === 'latency' ? series.latencyBars : series.lossBars
                    return <span key={metric} data-carrier-metric={metric}
                      className={vclass('grid items-end gap-[1px] opacity-80 group-hover:opacity-100', dual ? 'h-[2px]' : 'h-1')}
                      style={{ gridTemplateColumns: `repeat(${bars.length}, minmax(0, 1fr))` }}>
                      {bars.map(bar => <span key={bar.key} title={`${t(metric === 'latency' ? '延迟' : '丢包')} · ${bar.tooltip}`} aria-label={bar.tooltip} className={vclass('block h-full w-full rounded-[1px]', bar.className)} />)}
                    </span>
                  })}
                </span>
              </span>)}
            </span>
          </span>
        ))}
      </button>
    )
  }

  const row = (bars: typeof latencyRenderBars) => (
    <div className="group/panel relative items-center gap-1 opacity-80 hover:opacity-100">
      <div
        className="grid h-1 cursor-auto items-end gap-[1px] transition-all hover:h-2.5"
        style={{ gridTemplateColumns: `repeat(${bars.length}, minmax(0, 1fr))` }}
      >
        {bars.map(bar => (
          <span key={bar.key} title={bar.tooltip} aria-label={bar.tooltip} className="h-full w-full">
            <span className={vclass('block h-full w-full rounded-[1px] transition-all group-hover:opacity-50 hover:scale-y-160 hover:opacity-100', bar.className)} />
          </span>
        ))}
      </div>
    </div>
  )

  return (
    <button
      type="button"
      className="group flex w-full flex-col gap-[1px] pr-4 text-left"
      aria-label="打开延迟和丢包监测"
      onClick={click}
      onKeyDown={stop}
    >
      {row(latencyRenderBars)}
      {row(lossRenderBars)}
    </button>
  )
}
