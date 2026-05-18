/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  clearFlaggedHashes,
  deleteFlaggedHash,
  getFlaggedHashCount,
} from './api'

export function ContentModerationBlacklistPage() {
  const { t } = useTranslation()
  const [count, setCount] = useState<number>(0)
  const [hash, setHash] = useState<string>('')

  useEffect(() => {
    void load()
  }, [])

  async function load() {
    try {
      setCount(await getFlaggedHashCount())
    } catch (e: any) {
      toast.error(e?.message || 'failed')
    }
  }

  async function del() {
    if (!hash.trim()) return
    try {
      await deleteFlaggedHash(hash.trim())
      toast.success('Deleted')
      setHash('')
      void load()
    } catch (e: any) {
      toast.error(e?.message || 'failed')
    }
  }

  async function clearAll() {
    if (!window.confirm('Clear all flagged hashes?')) return
    try {
      await clearFlaggedHashes()
      toast.success(t('Blacklist cleared'))
      void load()
    } catch (e: any) {
      toast.error(e?.message || 'failed')
    }
  }

  return (
    <div className='space-y-4'>
      <div className='rounded border bg-card p-4'>
        <div className='text-sm'>{t('Flagged hash count')}</div>
        <div className='text-3xl font-semibold'>{count}</div>
      </div>
      <div className='rounded border bg-card p-4'>
        <h3 className='mb-2 font-semibold'>{t('Delete hash')}</h3>
        <div className='flex gap-2'>
          <input
            type='text'
            value={hash}
            onChange={(e) => setHash(e.target.value)}
            placeholder='sha256 hash'
            className='flex-1 rounded border px-2 py-1 font-mono text-xs'
          />
          <button
            type='button'
            onClick={del}
            className='rounded border px-3 py-1'
          >
            Delete
          </button>
        </div>
      </div>
      <div className='rounded border bg-card p-4'>
        <h3 className='mb-2 font-semibold'>{t('Clear all hashes')}</h3>
        <button
          type='button'
          onClick={clearAll}
          className='rounded bg-destructive px-3 py-1 text-destructive-foreground'
        >
          Clear all
        </button>
      </div>
    </div>
  )
}
