/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { createFileRoute } from '@tanstack/react-router'
import { ContentModerationBlacklistPage } from '@/features/content-moderation/blacklist-page'

export const Route = createFileRoute('/_authenticated/content-moderation/blacklist')({
  component: ContentModerationBlacklistPage,
})
