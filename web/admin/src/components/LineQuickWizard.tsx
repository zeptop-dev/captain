import { Alert, Button, Group, Modal, MultiSelect, NumberInput, Select, Stack, Text, TextInput } from '@mantine/core'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { type LineGroup, type Node } from '../lib/api'
import { type EntryNodeDetail, type InboundChoice, proxyServer } from '../lib/entry-flow'
import { quickLineGroup, type QuickLineDraft } from '../lib/quick-line'
import { useSettingsDirty } from '../lib/settings-draft'
export function LineQuickWizard({ nodes, details, inbounds, onClose, onContinue }: { nodes: Node[]; details: EntryNodeDetail[]; inbounds: InboundChoice[]; onClose: () => void; onContinue: (draft: QuickLineDraft) => void }) {
 const { t } = useTranslation()
 const [name, setName] = useState(''), [fronts, setFronts] = useState<string[]>([]), [relays, setRelays] = useState<string[]>([]), [target, setTarget] = useState<string | null>(null), [strategy, setStrategy] = useState<string | null>('failover'), [port, setPort] = useState<number | string>(20000)
 useSettingsDirty(!!name || fronts.length > 0 || relays.length > 0 || !!target)
 const landing = inbounds.find(c => String(c.ib.ID) === target)
 const options = nodes.filter(proxyServer).map(n => ({ value: String(n.id), label: n.name, disabled: n.id === landing?.ib.NodeID }))
 const valid = name.trim() && fronts.length > 0 && target && landing?.ib.Enabled && ![...fronts, ...relays].includes(String(landing.ib.NodeID)) && !fronts.some(n => relays.includes(n)) && Number.isInteger(Number(port)) && Number(port) > 0 && Number(port) < 65536
 const group = (ids: string[], policy: LineGroup['strategy']) => quickLineGroup(ids, policy, Number(port), details)
 return <Modal opened onClose={onClose} title={t('insights.lineWizard')} size="lg"><Stack><Alert color="blue">{t('insights.wizardHint')}</Alert><TextInput label={t('lines.name')} required value={name} onChange={e => setName(e.currentTarget.value)} /><MultiSelect label={t('insights.fronts')} searchable maxValues={8} data={options.map(n => ({ ...n, disabled: n.disabled || relays.includes(n.value) }))} value={fronts} onChange={setFronts} /><MultiSelect label={t('insights.relays')} description={t('insights.relaysHint')} searchable maxValues={8} data={options.map(n => ({ ...n, disabled: n.disabled || fronts.includes(n.value) }))} value={relays} onChange={setRelays} /><Select label={t('lines.strategy')} value={strategy} allowDeselect={false} onChange={setStrategy} data={['failover', 'roundrobin'].map(value => ({ value, label: t(`lines.strategies.${value}`) }))} /><Select label={t('entryFlow.targetInbound')} searchable value={target} onChange={setTarget} data={inbounds.filter(c => c.ib.Enabled).map(c => ({ value: String(c.ib.ID), label: `${c.node.name} / ${c.ib.Tag}`, disabled: [...fronts, ...relays].includes(String(c.ib.NodeID)) }))} /><NumberInput label={t('lines.listenPort')} description={t('insights.portHint')} value={port} min={1} max={65535} allowDecimal={false} onChange={setPort} /><Text size="xs" c="dimmed">{t('insights.wizardScope')}</Text><Group justify="flex-end"><Button disabled={!valid} onClick={() => onContinue({ name: name.trim(), target_inbound_id: Number(target), groups: [group(fronts, 'failover'), ...(relays.length ? [group(relays, strategy as LineGroup['strategy'])] : [])] })}>{t('insights.continueReview')}</Button></Group></Stack></Modal>
}
