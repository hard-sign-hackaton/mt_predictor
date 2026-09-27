import type { RiskThresholds } from './types'

export type RiskLevel = 'normal' | 'watch' | 'high'

/** Вычисляет отображаемую критичность по порогам, полученным от backend. */
export function riskFromDelaySeconds(delaySeconds: number, thresholds: RiskThresholds): RiskLevel {
  if (delaySeconds >= thresholds.highDelaySeconds) return 'high'
  if (delaySeconds >= thresholds.watchDelaySeconds) return 'watch'
  return 'normal'
}

/** Цвет маркера отражает модуль отклонения: и сильное опережение, и опоздание нарушают график. */
export function riskFromScheduleDeviation(delaySeconds: number | undefined, thresholds: RiskThresholds): RiskLevel | 'unknown' {
  if (delaySeconds === undefined) return 'unknown'
  const deviation = Math.abs(delaySeconds)
  if (deviation >= thresholds.highDelaySeconds) return 'high'
  if (deviation >= thresholds.watchDelaySeconds) return 'watch'
  return 'normal'
}
