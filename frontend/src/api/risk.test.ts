import { describe, expect, it } from 'vitest'
import { riskFromDelaySeconds } from './risk'

const thresholds = { watchDelaySeconds: 180, highDelaySeconds: 420 }

describe('riskFromDelaySeconds', () => {
  it('uses inclusive backend-configured boundaries', () => {
    expect(riskFromDelaySeconds(179, thresholds)).toBe('normal')
    expect(riskFromDelaySeconds(180, thresholds)).toBe('watch')
    expect(riskFromDelaySeconds(419, thresholds)).toBe('watch')
    expect(riskFromDelaySeconds(420, thresholds)).toBe('high')
  })
})
