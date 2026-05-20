/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/content-moderation/')({
  beforeLoad: () => {
    throw redirect({ to: '/system-settings/moderation/config' })
  },
})
