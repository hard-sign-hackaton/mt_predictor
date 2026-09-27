import { describe, expect, it } from 'vitest'
import { formatDelay, riskLabel } from '../utils'

describe('display helpers', () => {
  it('formats positive and negative delays', () => {
    expect(formatDelay(510)).toBe('+8:30')
    expect(formatDelay(-65)).toBe('−1:05')
  })

  it('keeps semantic risk labels independent from color', () => {
    expect(riskLabel.high).toBe('HIGH')
    expect(riskLabel.watch).toBe('WATCH')
    expect(riskLabel.normal).toBe('OK')
  })
})
