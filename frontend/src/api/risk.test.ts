import { describe, expect, it } from 'vitest'
import { riskFromDelaySeconds, riskFromScheduleDeviation } from './risk'

const thresholds = { watchDelaySeconds: 180, highDelaySeconds: 420 }

describe('riskFromDelaySeconds', () => {
  it('uses inclusive backend-configured boundaries', () => {
    expect(riskFromDelaySeconds(179, thresholds)).toBe('normal')
    expect(riskFromDelaySeconds(180, thresholds)).toBe('watch')
    expect(riskFromDelaySeconds(419, thresholds)).toBe('watch')
    expect(riskFromDelaySeconds(420, thresholds)).toBe('high')
  })
})

describe('riskFromScheduleDeviation', () => {
  it('colors both late and early deviations by their absolute size', () => {
    expect(riskFromScheduleDeviation(0, thresholds)).toBe('normal')
    expect(riskFromScheduleDeviation(-180, thresholds)).toBe('watch')
    expect(riskFromScheduleDeviation(419, thresholds)).toBe('watch')
    expect(riskFromScheduleDeviation(-420, thresholds)).toBe('high')
    expect(riskFromScheduleDeviation(undefined, thresholds)).toBe('unknown')
  })
})
