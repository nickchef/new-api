/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { createFileRoute } from '@tanstack/react-router'
import { ContentModerationStatusPage } from '@/features/content-moderation/status-page'

export const Route = createFileRoute('/_authenticated/system-settings/moderation/status')({
  component: ContentModerationStatusPage,
})
