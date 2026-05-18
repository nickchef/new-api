/*
Copyright (C) 2023-2026 QuantumNous

Licensed under the GNU Affero General Public License v3 or later.
*/
import { api } from '@/lib/api'

const ROOT = '/api/admin/content_moderation'

export interface ContentModerationConfig {
  enabled: boolean
  mode: 'off' | 'observe' | 'pre_block'
  base_url: string
  model: string
  api_key_count: number
  api_key_masks: string[]
  timeout_ms: number
  retry_count: number
  thresholds: Record<string, number>
  sample_rate: number
  input_scope: 'last_user' | 'all_user' | 'all_messages'
  pre_hash_check_enabled: boolean
  model_mode: 'all' | 'whitelist' | 'blacklist'
  model_list: string[]
  block_status: number
  block_message: string
  auto_ban_enabled: boolean
  ban_threshold: number
  violation_window_hours: number
  email_on_hit: boolean
  email_to_admin: boolean
  email_to_user: boolean
  worker_count: number
  queue_size: number
  record_non_hits: boolean
  hit_retention_days: number
  non_hit_retention_days: number
  categories: string[]
}

export type UpdateContentModerationConfigInput = Partial<
  Omit<ContentModerationConfig, 'api_key_count' | 'api_key_masks' | 'categories'>
> & {
  api_keys?: string[]
}

export interface ContentModerationLog {
  id: number
  request_id: string
  user_id: number
  username: string
  token_id: number
  token_name: string
  group: string
  endpoint: string
  provider: string
  model: string
  protocol: string
  mode: string
  action: string
  detection_layer: string
  flagged: boolean
  highest_category: string
  highest_score: number
  category_scores: string
  threshold_snapshot: string
  input_excerpt: string
  input_hash: string
  upstream_latency_ms: number
  queue_delay_ms: number
  error: string
  violation_count: number
  auto_banned: boolean
  email_sent: boolean
  created_at: number
}

export interface ContentModerationStatusResponse {
  enabled: boolean
  mode: string
  worker: {
    active_workers: number
    idle_workers: number
    queue_length: number
    queue_capacity: number
    enqueued: number
    dropped: number
    processed: number
    errors: number
  }
  api_keys: Array<{
    index: number
    masked: string
    healthy: boolean
    failure_count: number
    success_count: number
    last_error: string
    last_latency_ms: number
    last_http_status: number
    frozen_until?: string
  }>
  flagged_hash_count: number
  metrics: {
    requests_total: Record<string, number>
    openai_latency_avg_ms: number
    openai_latency_count: number
    openai_errors_total: Record<string, number>
    auto_bans_total: number
  }
}

export async function getContentModerationConfig() {
  const res = await api.get<{ success: boolean; data: ContentModerationConfig }>(
    `${ROOT}/config`,
  )
  return res.data.data
}

export async function updateContentModerationConfig(
  input: UpdateContentModerationConfigInput,
) {
  const res = await api.put<{ success: boolean }>(`${ROOT}/config`, input)
  return res.data
}

export async function getContentModerationStatus() {
  const res = await api.get<{ success: boolean; data: ContentModerationStatusResponse }>(
    `${ROOT}/status`,
  )
  return res.data.data
}

export async function testContentModerationKeys(input: {
  api_keys?: string[]
  base_url?: string
  model?: string
  prompt?: string
}) {
  const res = await api.post<{ success: boolean; data: any; message?: string }>(
    `${ROOT}/test_api_keys`,
    input,
  )
  return res.data
}

export async function previewContentModeration(input: {
  text: string
  images?: string[]
}) {
  const res = await api.post<{ success: boolean; data: any; message?: string }>(
    `${ROOT}/preview`,
    input,
  )
  return res.data
}

export interface ContentModerationLogQuery {
  user_id?: number
  token_id?: number
  flagged?: boolean
  layer?: string
  start?: number
  end?: number
  page?: number
  page_size?: number
}

export async function listContentModerationLogs(q: ContentModerationLogQuery) {
  const params: Record<string, string | number> = {}
  if (q.user_id) params.user_id = q.user_id
  if (q.token_id) params.token_id = q.token_id
  if (q.flagged !== undefined) params.flagged = q.flagged ? 'true' : 'false'
  if (q.layer) params.layer = q.layer
  if (q.start) params.start = q.start
  if (q.end) params.end = q.end
  if (q.page) params.page = q.page
  if (q.page_size) params.page_size = q.page_size
  const res = await api.get<{
    success: boolean
    data: { total: number; items: ContentModerationLog[] }
  }>(`${ROOT}/logs`, { params })
  return res.data.data
}

export async function getContentModerationLog(id: number) {
  const res = await api.get<{ success: boolean; data: ContentModerationLog }>(
    `${ROOT}/logs/${id}`,
  )
  return res.data.data
}

export async function getFlaggedHashCount() {
  const res = await api.get<{ success: boolean; data: { count: number } }>(
    `${ROOT}/flagged_hash/count`,
  )
  return res.data.data.count
}

export async function deleteFlaggedHash(hash: string) {
  const res = await api.delete<{ success: boolean }>(
    `${ROOT}/flagged_hash`,
    { data: { hash } },
  )
  return res.data
}

export async function clearFlaggedHashes() {
  const res = await api.post<{ success: boolean }>(
    `${ROOT}/flagged_hash/clear`,
  )
  return res.data
}

export async function unbanContentModerationUser(userId: number) {
  const res = await api.post<{ success: boolean }>(
    `${ROOT}/unban/${userId}`,
  )
  return res.data
}

export async function getViolationCount(userId: number) {
  const res = await api.get<{
    success: boolean
    data: {
      user_id: number
      count: number
      window_hours: number
      threshold: number
    }
  }>(`${ROOT}/violation_count/${userId}`)
  return res.data.data
}
