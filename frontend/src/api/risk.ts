import type { RiskThresholds } from './types'

export type RiskLevel = 'normal' | 'watch' | 'high'

/** Вычисляет отображаемую критичность по порогам, полученным от backend. */
export function riskFromDelaySeconds(delaySeconds: number, thresholds: RiskThresholds): RiskLevel {
  if (delaySeconds >= thresholds.highDelaySeconds) return 'high'
  if (delaySeconds >= thresholds.watchDelaySeconds) return 'watch'
  return 'normal'
}
