/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  getContentModerationConfig,
  testContentModerationKeys,
  updateContentModerationConfig,
  type ContentModerationConfig,
} from './api'

export function ContentModerationConfigPage() {
  const { t } = useTranslation()
  const [config, setConfig] = useState<ContentModerationConfig | null>(null)
  const [newKeys, setNewKeys] = useState<string>('')
  const [thresholds, setThresholds] = useState<Record<string, number>>({})
  const [modelList, setModelList] = useState<string>('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    void load()
  }, [])

  async function load() {
    try {
      const data = await getContentModerationConfig()
      setConfig(data)
      setThresholds({ ...data.thresholds })
      setModelList((data.model_list || []).join('\n'))
    } catch (e: any) {
      toast.error(e?.message || 'failed to load')
    }
  }

  async function save() {
    if (!config) return
    setSaving(true)
    try {
      const apiKeysToSend = newKeys
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean)
      const payload: any = {
        enabled: config.enabled,
        mode: config.mode,
        base_url: config.base_url,
        model: config.model,
        timeout_ms: config.timeout_ms,
        retry_count: config.retry_count,
        thresholds,
        sample_rate: config.sample_rate,
        input_scope: config.input_scope,
        pre_hash_check_enabled: config.pre_hash_check_enabled,
        model_mode: config.model_mode,
        model_list: modelList
          .split(/\r?\n/)
          .map((s) => s.trim())
          .filter(Boolean),
        block_status: config.block_status,
        block_message: config.block_message,
        auto_ban_enabled: config.auto_ban_enabled,
        ban_threshold: config.ban_threshold,
        violation_window_hours: config.violation_window_hours,
        email_on_hit: config.email_on_hit,
        email_to_admin: config.email_to_admin,
        email_to_user: config.email_to_user,
        worker_count: config.worker_count,
        queue_size: config.queue_size,
        record_non_hits: config.record_non_hits,
        hit_retention_days: config.hit_retention_days,
        non_hit_retention_days: config.non_hit_retention_days,
      }
      if (apiKeysToSend.length > 0) {
        payload.api_keys = apiKeysToSend
      }
      const res = await updateContentModerationConfig(payload)
      if (!res?.success) {
        // Error toast already shown by the response interceptor
        return
      }
      toast.success(t('Save successful'))
      setNewKeys('')
      void load()
    } catch (e: any) {
      toast.error(e?.message || 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function testKeys() {
    try {
      const apiKeysToSend = newKeys
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean)
      const res = await testContentModerationKeys({
        api_keys: apiKeysToSend.length > 0 ? apiKeysToSend : undefined,
        prompt: 'hello world',
      })
      if (res.success) {
        toast.success(t('API key test succeeded'))
      } else {
        toast.error(res.message || 'test failed')
      }
    } catch (e: any) {
      toast.error(e?.message || 'test failed')
    }
  }

  if (!config) {
    return <div className='p-4'>Loading...</div>
  }

  return (
    <div className='space-y-6 pb-24'>
      <section className='rounded border bg-card p-4'>
        <h2 className='mb-3 text-lg font-semibold'>Basics</h2>
        <label className='flex items-center gap-2'>
          <input
            type='checkbox'
            checked={config.enabled}
            onChange={(e) => setConfig({ ...config, enabled: e.target.checked })}
          />
          {t('Content moderation enabled')}
        </label>
        <div className='mt-2 flex items-center gap-2'>
          <label>{t('Content moderation mode')}:</label>
          <select
            value={config.mode}
            onChange={(e) => setConfig({ ...config, mode: e.target.value as any })}
            className='rounded border px-2 py-1'
          >
            <option value='off'>off</option>
            <option value='observe'>observe</option>
            <option value='pre_block'>pre_block</option>
          </select>
        </div>
      </section>

      <section className='rounded border bg-card p-4'>
        <h2 className='mb-3 text-lg font-semibold'>OpenAI</h2>
        <div className='space-y-2'>
          <Field
            label={t('Moderation Base URL')}
            value={config.base_url}
            onChange={(v) => setConfig({ ...config, base_url: v })}
          />
          <Field
            label={t('Moderation Model')}
            value={config.model}
            onChange={(v) => setConfig({ ...config, model: v })}
          />
          <div>
            <div className='mb-1 text-sm'>
              {t('Moderation API Keys')}（{config.api_key_count} configured：
              {config.api_key_masks.join(', ')}）
            </div>
            <textarea
              rows={3}
              placeholder='sk-xxx (one per line)'
              value={newKeys}
              onChange={(e) => setNewKeys(e.target.value)}
              className='w-full rounded border px-2 py-1 font-mono text-xs'
            />
            <button
              type='button'
              onClick={testKeys}
              className='mt-1 rounded border px-3 py-1 text-sm'
            >
              {t('Test API keys')}
            </button>
          </div>
          <NumberField
            label={t('Timeout (ms)')}
            value={config.timeout_ms}
            onChange={(v) => setConfig({ ...config, timeout_ms: v })}
          />
          <NumberField
            label={t('Retry count')}
            value={config.retry_count}
            onChange={(v) => setConfig({ ...config, retry_count: v })}
          />
        </div>
      </section>

      <section className='rounded border bg-card p-4'>
        <h2 className='mb-3 text-lg font-semibold'>{t('Thresholds (13 categories)')}</h2>
        <table className='w-full text-sm'>
          <thead>
            <tr>
              <th className='text-left'>Category</th>
              <th>Threshold</th>
            </tr>
          </thead>
          <tbody>
            {config.categories.map((cat) => (
              <tr key={cat}>
                <td className='py-1'>{cat}</td>
                <td className='py-1'>
                  <input
                    type='number'
                    step={0.01}
                    min={0}
                    max={1}
                    value={thresholds[cat] ?? 0}
                    onChange={(e) =>
                      setThresholds({ ...thresholds, [cat]: parseFloat(e.target.value) })
                    }
                    className='w-24 rounded border px-2 py-1'
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section className='rounded border bg-card p-4'>
        <h2 className='mb-3 text-lg font-semibold'>Limits</h2>
        <NumberField
          label={t('Sample rate (%)')}
          value={config.sample_rate}
          onChange={(v) => setConfig({ ...config, sample_rate: v })}
        />
        <div className='mt-2 flex items-center gap-2'>
          <label>{t('Input scope')}:</label>
          <select
            value={config.input_scope}
            onChange={(e) =>
              setConfig({ ...config, input_scope: e.target.value as any })
            }
            className='rounded border px-2 py-1'
          >
            <option value='last_user'>{t('Last user message only')}</option>
            <option value='all_user'>{t('All user messages')}</option>
            <option value='all_messages'>{t('All messages')}</option>
          </select>
        </div>
        <label className='mt-2 flex items-center gap-2'>
          <input
            type='checkbox'
            checked={config.pre_hash_check_enabled}
            onChange={(e) =>
              setConfig({ ...config, pre_hash_check_enabled: e.target.checked })
            }
          />
          {t('Pre-hash check enabled')}
        </label>
        <div className='mt-2 flex items-center gap-2'>
          <label>{t('Model scope mode')}:</label>
          <select
            value={config.model_mode}
            onChange={(e) => setConfig({ ...config, model_mode: e.target.value as any })}
            className='rounded border px-2 py-1'
          >
            <option value='all'>{t('All models')}</option>
            <option value='whitelist'>{t('Whitelist')}</option>
            <option value='blacklist'>{t('Blacklist')}</option>
          </select>
        </div>
        <div className='mt-2'>
          <div className='mb-1 text-sm'>{t('Model patterns')} (one per line)</div>
          <textarea
            rows={3}
            value={modelList}
            onChange={(e) => setModelList(e.target.value)}
            className='w-full rounded border px-2 py-1 font-mono text-xs'
          />
        </div>
      </section>

      <section className='rounded border bg-card p-4'>
        <h2 className='mb-3 text-lg font-semibold'>Disposition</h2>
        <NumberField
          label={t('Block HTTP status')}
          value={config.block_status}
          onChange={(v) => setConfig({ ...config, block_status: v })}
        />
        <Field
          label={t('Block message')}
          value={config.block_message}
          onChange={(v) => setConfig({ ...config, block_message: v })}
        />
        <label className='mt-2 flex items-center gap-2'>
          <input
            type='checkbox'
            checked={config.auto_ban_enabled}
            onChange={(e) => setConfig({ ...config, auto_ban_enabled: e.target.checked })}
          />
          {t('Auto-ban enabled')}
        </label>
        <NumberField
          label={t('Ban threshold')}
          value={config.ban_threshold}
          onChange={(v) => setConfig({ ...config, ban_threshold: v })}
        />
        <NumberField
          label={t('Violation window (hours)')}
          value={config.violation_window_hours}
          onChange={(v) => setConfig({ ...config, violation_window_hours: v })}
        />
        <label className='mt-2 flex items-center gap-2'>
          <input
            type='checkbox'
            checked={config.email_on_hit}
            onChange={(e) => setConfig({ ...config, email_on_hit: e.target.checked })}
          />
          {t('Email on hit')}
        </label>
        <label className='mt-2 flex items-center gap-2'>
          <input
            type='checkbox'
            checked={config.email_to_admin}
            onChange={(e) => setConfig({ ...config, email_to_admin: e.target.checked })}
          />
          {t('Email admin')}
        </label>
        <label className='mt-2 flex items-center gap-2'>
          <input
            type='checkbox'
            checked={config.email_to_user}
            onChange={(e) => setConfig({ ...config, email_to_user: e.target.checked })}
          />
          {t('Email user')}
        </label>
      </section>

      <section className='rounded border bg-card p-4'>
        <h2 className='mb-3 text-lg font-semibold'>Async</h2>
        <NumberField
          label={t('Worker count')}
          value={config.worker_count}
          onChange={(v) => setConfig({ ...config, worker_count: v })}
        />
        <NumberField
          label={t('Queue size')}
          value={config.queue_size}
          onChange={(v) => setConfig({ ...config, queue_size: v })}
        />
      </section>

      <section className='rounded border bg-card p-4'>
        <h2 className='mb-3 text-lg font-semibold'>Logs</h2>
        <label className='flex items-center gap-2'>
          <input
            type='checkbox'
            checked={config.record_non_hits}
            onChange={(e) => setConfig({ ...config, record_non_hits: e.target.checked })}
          />
          {t('Record non-hits')}
        </label>
        <NumberField
          label={t('Hit log retention (days)')}
          value={config.hit_retention_days}
          onChange={(v) => setConfig({ ...config, hit_retention_days: v })}
        />
        <NumberField
          label={t('Non-hit log retention (days)')}
          value={config.non_hit_retention_days}
          onChange={(v) => setConfig({ ...config, non_hit_retention_days: v })}
        />
      </section>

      <div className='sticky bottom-0 -mx-4 flex items-center justify-end gap-2 border-t bg-background/95 px-4 py-3 backdrop-blur'>
        <button
          type='button'
          onClick={() => void load()}
          disabled={saving}
          className='rounded border px-4 py-2 text-sm font-medium disabled:opacity-60'
        >
          {t('Reset')}
        </button>
        <button
          type='button'
          onClick={save}
          disabled={saving}
          className='rounded bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60'
        >
          {saving ? t('Saving...') : t('Save Changes')}
        </button>
      </div>
    </div>
  )
}

function Field({
  label,
  value,
  onChange,
}: {
  label: string
  value: string
  onChange: (v: string) => void
}) {
  return (
    <div className='flex items-center gap-2'>
      <label className='w-40 text-sm'>{label}</label>
      <input
        type='text'
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className='flex-1 rounded border px-2 py-1'
      />
    </div>
  )
}

function NumberField({
  label,
  value,
  onChange,
}: {
  label: string
  value: number
  onChange: (v: number) => void
}) {
  return (
    <div className='flex items-center gap-2'>
      <label className='w-40 text-sm'>{label}</label>
      <input
        type='number'
        value={value}
        onChange={(e) => onChange(parseInt(e.target.value, 10) || 0)}
        className='w-32 rounded border px-2 py-1'
      />
    </div>
  )
}
