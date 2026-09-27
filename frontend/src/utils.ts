import type { ActionStatus, IncidentStatus, RiskLevel, TelemetryState } from './types'

export const riskLabel: Record<RiskLevel, string> = {
  normal: 'OK',
  watch: 'WATCH',
  high: 'HIGH',
  unknown: 'НЕТ ПРОГНОЗА',
}

export const incidentStatusLabel: Record<IncidentStatus, string> = {
  new: 'Новый',
  in_progress: 'В работе',
  closed: 'Закрыт',
}

export const actionStatusLabel: Record<ActionStatus, string> = {
  recommended: 'Рекомендовано',
  registered: 'Зарегистрировано',
  completed: 'Выполнено',
  cancelled: 'Отменено',
  mock: 'Mock',
}

export const telemetryLabel: Record<TelemetryState, string> = {
  live: 'live',
  stale: 'stale',
  unmapped: 'unmapped',
  invalid_location: 'invalid location',
}

export function formatDelay(seconds: number): string {
  const sign = seconds >= 0 ? '+' : '−'
  const absolute = Math.abs(seconds)
  const minutes = Math.floor(absolute / 60)
  const rest = absolute % 60
  return `${sign}${minutes}:${String(rest).padStart(2, '0')}`
}

export function formatTime(iso: string): string {
  return new Intl.DateTimeFormat('ru-RU', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(iso))
}

export function secondsAgo(iso: string, now = Date.now()): number {
  return Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
}
