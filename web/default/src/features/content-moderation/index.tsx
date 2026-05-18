/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { Link, Outlet, useLocation } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout'

const TABS = [
  { id: 'config', path: '/content-moderation/config', labelKey: 'Content Moderation' },
  { id: 'logs', path: '/content-moderation/logs', labelKey: 'Moderation Logs' },
  { id: 'blacklist', path: '/content-moderation/blacklist', labelKey: 'Hash Blacklist' },
  { id: 'status', path: '/content-moderation/status', labelKey: 'Runtime Status' },
] as const

export function ContentModerationLayout() {
  const { t } = useTranslation()
  const location = useLocation()
  return (
    <Main>
      <div className='min-h-0 flex-1 px-4 pt-6 pb-4'>
        <h1 className='mb-4 text-2xl font-semibold'>{t('Content Moderation')}</h1>
        <div className='mb-4 flex gap-2 border-b'>
          {TABS.map((tab) => {
            const active = location.pathname.startsWith(tab.path)
            return (
              <Link
                key={tab.id}
                to={tab.path}
                className={
                  'border-b-2 px-3 py-2 text-sm font-medium ' +
                  (active
                    ? 'border-primary text-primary'
                    : 'border-transparent text-muted-foreground hover:text-foreground')
                }
              >
                {t(tab.labelKey)}
              </Link>
            )
          })}
        </div>
        <Outlet />
      </div>
    </Main>
  )
}
