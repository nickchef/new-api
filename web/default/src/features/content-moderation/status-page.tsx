/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getContentModerationStatus, type ContentModerationStatusResponse } from './api'

export function ContentModerationStatusPage() {
  const { t } = useTranslation()
  const [data, setData] = useState<ContentModerationStatusResponse | null>(null)

  useEffect(() => {
    void load()
    const id = setInterval(load, 5000)
    return () => clearInterval(id)
  }, [])

  async function load() {
    try {
      setData(await getContentModerationStatus())
    } catch {
      // 静默：未启用或 admin 未登录都可能 401
    }
  }

  if (!data) {
    return <div className='p-4'>Loading...</div>
  }

  return (
    <div className='space-y-4'>
      <div className='grid grid-cols-2 gap-4 md:grid-cols-4'>
        <Stat label='Enabled' value={String(data.enabled)} />
        <Stat label='Mode' value={data.mode} />
        <Stat label={t('Flagged hash count')} value={String(data.flagged_hash_count)} />
        <Stat label={t('Active workers')} value={String(data.worker.active_workers)} />
        <Stat label={t('Queue length')} value={String(data.worker.queue_length)} />
        <Stat label={t('Enqueued')} value={String(data.worker.enqueued)} />
        <Stat label={t('Dropped')} value={String(data.worker.dropped)} />
        <Stat label={t('Processed')} value={String(data.worker.processed)} />
      </div>

      <div className='rounded border bg-card p-4'>
        <h3 className='mb-2 font-semibold'>{t('Moderation API Keys')}</h3>
        <table className='w-full text-sm'>
          <thead>
            <tr className='border-b'>
              <th className='text-left'>Index</th>
              <th className='text-left'>Masked</th>
              <th className='text-left'>Healthy</th>
              <th className='text-left'>Success</th>
              <th className='text-left'>Failure</th>
              <th className='text-left'>Last status</th>
              <th className='text-left'>Last latency (ms)</th>
              <th className='text-left'>Frozen until</th>
            </tr>
          </thead>
          <tbody>
            {data.api_keys.map((k) => (
              <tr key={k.index} className='border-b'>
                <td>{k.index}</td>
                <td className='font-mono'>{k.masked}</td>
                <td>{k.healthy ? '✓' : '✗'}</td>
                <td>{k.success_count}</td>
                <td>{k.failure_count}</td>
                <td>{k.last_http_status}</td>
                <td>{k.last_latency_ms}</td>
                <td>{k.frozen_until || '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className='rounded border bg-card p-4'>
        <h3 className='mb-2 font-semibold'>Metrics</h3>
        <div className='text-sm'>
          <div>auto_bans_total: {data.metrics.auto_bans_total}</div>
          <div>
            openai_latency_avg_ms: {data.metrics.openai_latency_avg_ms.toFixed(1)} (n=
            {data.metrics.openai_latency_count})
          </div>
          <div className='mt-2 font-semibold'>requests_total by layer|mode|action</div>
          <pre className='whitespace-pre-wrap break-all'>
            {JSON.stringify(data.metrics.requests_total, null, 2)}
          </pre>
          <div className='mt-2 font-semibold'>openai_errors_total</div>
          <pre className='whitespace-pre-wrap break-all'>
            {JSON.stringify(data.metrics.openai_errors_total, null, 2)}
          </pre>
        </div>
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className='rounded border bg-card p-3'>
      <div className='text-xs text-muted-foreground'>{label}</div>
      <div className='text-2xl font-semibold'>{value}</div>
    </div>
  )
}
