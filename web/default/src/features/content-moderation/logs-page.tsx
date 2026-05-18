/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  listContentModerationLogs,
  unbanContentModerationUser,
  type ContentModerationLog,
} from './api'

export function ContentModerationLogsPage() {
  const { t } = useTranslation()
  const [items, setItems] = useState<ContentModerationLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [userId, setUserId] = useState<string>('')
  const [flagged, setFlagged] = useState<string>('')
  const [selected, setSelected] = useState<ContentModerationLog | null>(null)

  useEffect(() => {
    void load()
  }, [page])

  async function load() {
    try {
      const q: any = { page, page_size: pageSize }
      if (userId) q.user_id = parseInt(userId, 10)
      if (flagged) q.flagged = flagged === 'true'
      const res = await listContentModerationLogs(q)
      setItems(res.items || [])
      setTotal(res.total || 0)
    } catch (e: any) {
      toast.error(e?.message || 'failed to load')
    }
  }

  async function unban(uid: number) {
    if (!window.confirm('Unban user ' + uid + '?')) return
    try {
      await unbanContentModerationUser(uid)
      toast.success(t('User unbanned'))
      setSelected(null)
      void load()
    } catch (e: any) {
      toast.error(e?.message || 'failed')
    }
  }

  return (
    <div className='space-y-4'>
      <div className='flex items-center gap-2'>
        <input
          type='text'
          placeholder='user_id'
          value={userId}
          onChange={(e) => setUserId(e.target.value)}
          className='rounded border px-2 py-1'
        />
        <select
          value={flagged}
          onChange={(e) => setFlagged(e.target.value)}
          className='rounded border px-2 py-1'
        >
          <option value=''>All</option>
          <option value='true'>Flagged</option>
          <option value='false'>Allowed</option>
        </select>
        <button
          type='button'
          onClick={() => {
            setPage(1)
            void load()
          }}
          className='rounded border px-3 py-1'
        >
          Search
        </button>
      </div>
      <div className='text-sm text-muted-foreground'>Total: {total}</div>
      <table className='w-full text-sm'>
        <thead>
          <tr className='border-b'>
            <th className='text-left'>Time</th>
            <th className='text-left'>User</th>
            <th className='text-left'>Protocol</th>
            <th className='text-left'>Model</th>
            <th className='text-left'>{t('Detection layer')}</th>
            <th className='text-left'>{t('Highest category')}</th>
            <th className='text-left'>{t('Highest score')}</th>
            <th className='text-left'>Action</th>
          </tr>
        </thead>
        <tbody>
          {items.map((row) => (
            <tr
              key={row.id}
              className='cursor-pointer border-b hover:bg-muted/30'
              onClick={() => setSelected(row)}
            >
              <td className='py-1'>{new Date(row.created_at * 1000).toLocaleString()}</td>
              <td>{row.user_id}</td>
              <td>{row.protocol}</td>
              <td>{row.model}</td>
              <td>{row.detection_layer}</td>
              <td>{row.highest_category}</td>
              <td>{row.highest_score?.toFixed(3)}</td>
              <td>{row.action}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className='flex gap-2'>
        <button
          type='button'
          disabled={page <= 1}
          onClick={() => setPage(page - 1)}
          className='rounded border px-3 py-1 disabled:opacity-50'
        >
          Prev
        </button>
        <div className='self-center text-sm'>Page {page}</div>
        <button
          type='button'
          disabled={page * pageSize >= total}
          onClick={() => setPage(page + 1)}
          className='rounded border px-3 py-1 disabled:opacity-50'
        >
          Next
        </button>
      </div>

      {selected && (
        <div
          className='fixed inset-0 z-50 flex items-center justify-center bg-black/40'
          onClick={() => setSelected(null)}
        >
          <div
            className='max-h-[80vh] w-[600px] overflow-auto rounded bg-card p-4'
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className='mb-2 text-lg font-semibold'>Log #{selected.id}</h3>
            <pre className='whitespace-pre-wrap break-all text-xs'>
              {JSON.stringify(selected, null, 2)}
            </pre>
            {selected.auto_banned && (
              <button
                type='button'
                onClick={() => unban(selected.user_id)}
                className='mt-3 rounded bg-destructive px-3 py-1 text-destructive-foreground'
              >
                {t('Unban user')}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
