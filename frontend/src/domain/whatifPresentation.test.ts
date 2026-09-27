import { describe, expect, it } from 'vitest'
import type { WhatIfReport } from '../api/types'
import {
  whatIfDistance,
  whatIfDuration,
  whatIfModeLabel,
  whatIfReduction,
  whatIfUnavailableText,
  whatIfVerdictLabel,
  whatIfVerdictTone,
} from './whatifPresentation'

function report(overrides: Partial<WhatIfReport> = {}): WhatIfReport {
  return {
    mode: 'relieve', candidate: { unitId: 1, matchStatus: 'no_schedule', freshness: 'live', eventTime: '2026-01-06T10:00:00Z' },
    target: { occurrenceId: 'occ', trId: 1, routePatternId: 'p', joinCallIndex: 0, joinActionItemId: 1, joinStopId: 's', joinStopAddress: 'S', joinPlannedAt: '2026-01-06T10:00:00Z', totalCalls: 4, uniqueStops: 2 },
    deadhead: { assumedEmptySpeedKmh: 30 }, conflict: { detected: false },
    partition: { callsVehicleOne: 2, callsVehicleTwo: 2, stopsServedByOne: 0, stopsServedByTwo: 2 },
    serviceInterval: { improvedStops: [] }, scheduleReliefSeconds: null,
    unavailableReason: 'no_capacity_model', assumptions: [], generatedAt: '2026-01-06T10:00:00Z',
    ...overrides,
  }
}

describe('whatifPresentation', () => {
  it('переводит вердикты в формулировки и не выдаёт «не рассчитано» за «успевает»', () => {
    expect(whatIfVerdictLabel('feasible')).toBe('Успевает')
    expect(whatIfVerdictLabel('infeasible')).toBe('Не успевает')
    expect(whatIfVerdictLabel('stale_position')).toBe('Позиция устарела')
    expect(whatIfVerdictLabel(undefined)).toBe('Не рассчитано')
    expect(whatIfVerdictTone('feasible')).toBe('whatif__verdict--ok')
    expect(whatIfVerdictTone('infeasible')).toBe('whatif__verdict--bad')
    expect(whatIfVerdictTone(undefined)).toBe('whatif__verdict--missing')
  })

  // Исходный дефект: для исторического расписания запас доходил до −6329 ч и
  // выглядел как обычное число, хотя означал лишь расхождение часов с расписанием.
  it('называет прошедший и слишком далёкий рейс отдельно, а не «не успевает»', () => {
    expect(whatIfVerdictLabel('run_in_past')).toBe('Рейс уже прошёл')
    expect(whatIfVerdictLabel('run_too_far')).toBe('Рейс слишком далеко')
    expect(whatIfVerdictTone('run_in_past')).toBe('whatif__verdict--missing')
    expect(whatIfVerdictTone('run_too_far')).toBe('whatif__verdict--missing')
  })

  it('называет режимы разными словами, потому что эффекты у них разные', () => {
    expect(whatIfModeLabel('relieve')).toBe('Разделение рейса')
    expect(whatIfModeLabel('duplicate')).toBe('Дублирование рейса')
  })

  it('объясняет ограничение данных, а не отказ системы', () => {
    expect(whatIfUnavailableText(report())).toContain('ровно одно ТС на маршрут')
    expect(whatIfUnavailableText(report({ unavailableReason: 'что-то другое' }))).toContain('неполных данных')
  })

  it('показывает отсутствие данных как «нет данных», а не как ноль', () => {
    expect(whatIfDuration(undefined)).toBe('нет данных')
    expect(whatIfDistance(undefined)).toBe('нет данных')
    expect(whatIfDuration(0)).toBe('0 с')
    expect(whatIfDistance(0)).toBe('0 м')
  })

  it('форматирует длительности и расстояния в единицах Moscow', () => {
    expect(whatIfDuration(45)).toBe('45 с')
    expect(whatIfDuration(95)).toBe('1 мин 35 с')
    expect(whatIfDuration(-95)).toBe('−1 мин 35 с')
    expect(whatIfDuration(3900)).toBe('1 ч 05 мин')
    expect(whatIfDistance(840)).toBe('840 м')
    expect(whatIfDistance(1840.2)).toBe('1.8 км')
  })

  it('различает нулевое сокращение интервала и его отсутствие', () => {
    expect(whatIfReduction(0)).toBe('не изменился')
    expect(whatIfReduction(0.6667)).toBe('−67%')
    expect(whatIfReduction(undefined)).toBe('—')
  })
})
