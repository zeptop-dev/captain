import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { IconArrowUpRight, IconDownload } from '@tabler/icons-react'
import { catalogPlatforms, clientDownloadURL, clientFileKey, clientFileLabel, clientSubscriptionFormat, clientsForPlatform, initialClientPlatform, selectedClientFile, type PublishedClient } from '../../../shared/client-downloads'
import styles from './ClientCatalog.module.css'

function ClientCard({ client }: { client: PublishedClient }) {
  const { t } = useTranslation()
  const [selection, setSelection] = useState('')
  const file = selectedClientFile(client.downloads, selection)
  const url = file && clientDownloadURL(file.url)
  const homepage = clientDownloadURL(client.homepage)
  const format = clientSubscriptionFormat(client.format)
  return <article className={styles.card}>
    <div className={styles.cardHeading}><h3>{client.name}</h3>{client.recommended && <span className={styles.badge}>{t('clientCatalog.recommended')}</span>}</div>
    <p className={styles.format}>{t(`clientCatalog.format.${format}`)}</p>
    <div className={styles.meta}><span>{t(`clientCatalog.source.${client.source}`)}</span>{(file?.version || client.version) && <span>{file?.version || client.version}</span>}</div>
    {client.downloads.length > 1 ? <label className={styles.package}>{t('clientCatalog.package')}<select value={file ? clientFileKey(file) : ''} onChange={event => setSelection(event.target.value)}><option value="">{t('clientCatalog.choosePackage')}</option>{client.downloads.map(item => <option key={clientFileKey(item)} value={clientFileKey(item)}>{clientFileLabel(item)}</option>)}</select></label> : file && <p className={styles.file}>{clientFileLabel(file)}</p>}
    <div className={styles.actions}>{url ? <a className={styles.download} href={url} target="_blank" rel="noopener noreferrer">{client.source === 'store' ? t('clientCatalog.openStore') : t('clientCatalog.download')}<IconDownload size={16} aria-hidden /></a> : <button className={styles.download} type="button" disabled>{client.downloads.length > 1 ? t('clientCatalog.choosePackage') : t('clientCatalog.unavailable')}</button>}{homepage && <a className={styles.homepage} href={homepage} target="_blank" rel="noopener noreferrer">{t('clientCatalog.project')}<IconArrowUpRight size={15} aria-hidden /></a>}</div>
    {client.source === 'store' && <p className={styles.note}>{t('clientCatalog.storeNote')}</p>}
  </article>
}

export function ClientCatalog({ clients }: { clients: PublishedClient[] }) {
  const { t } = useTranslation()
  const available = catalogPlatforms(clients)
  const [choice, setChoice] = useState(() => initialClientPlatform(clients, navigator.userAgent, navigator.maxTouchPoints))
  const platform = available.includes(choice) ? choice : (available[0] ?? choice)
  const visible = clientsForPlatform(clients, platform)
  const recommended = visible.filter(client => client.recommended)
  const alternatives = visible.filter(client => !client.recommended)
  if (!clients.length) return <p>{t('clientCatalog.empty')}</p>
  return <div className={styles.catalog}>
    <div className={styles.platforms} role="group" aria-label={t('clientCatalog.platform')}>{available.map(value => <button type="button" key={value} aria-pressed={platform === value} onClick={() => setChoice(value)}>{t(`clientCatalog.platforms.${value}`)}</button>)}</div>
    <p className={styles.hint}>{t('clientCatalog.architectureHint')}</p>
    <div className={styles.cards}>{(recommended.length ? recommended : alternatives).map(client => <ClientCard key={`${client.id}:${platform}`} client={client} />)}</div>
    {recommended.length > 0 && alternatives.length > 0 && <details className={styles.alternatives} key={platform}><summary>{t('clientCatalog.otherChoices', { count: alternatives.length })}</summary><div className={styles.cards}>{alternatives.map(client => <ClientCard key={`${client.id}:${platform}`} client={client} />)}</div></details>}
  </div>
}
