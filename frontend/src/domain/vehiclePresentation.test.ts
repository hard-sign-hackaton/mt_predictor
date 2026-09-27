import { describe, expect, it } from 'vitest'
import type { MatchStatus, VehicleState } from '../api/types'
import { filterVehicles, matchStatusLabel, vehicleName } from './vehiclePresentation'

function vehicle(unitId: number, matchStatus: MatchStatus, currentDelaySeconds?: number): VehicleState {
  return {
    unitId, matchStatus, currentDelaySeconds, speedKmh: 0, headingDegrees: 0,
    eventTime: '2026-09-27T00:00:00Z', receivedAt: '2026-09-27T00:00:00Z',
    historical: false, freshness: 'live',
  }
}

describe('операторское представление транспорта', () => {
  it('показывает идентификатор конкретного ТС', () => {
    expect(vehicleName(vehicle(10, 'matched'))).toBe('ТС 10')
  })

  it('не выводит технические match-статусы', () => {
    expect(matchStatusLabel('matched_spatial')).toBe('Идёт по маршруту')
    expect(matchStatusLabel('off_route')).toBe('Вне маршрута')
    expect(matchStatusLabel('invalid_location')).toBe('Нет геопозиции')
  })

  it('совмещает маршрутный фильтр и подтверждённую задержку', () => {
    const vehicles = [
      vehicle(1, 'matched_spatial', 120),
      vehicle(2, 'matched', 0),
      vehicle(3, 'off_route', 60),
      vehicle(4, 'invalid_location'),
    ]
    expect(filterVehicles(vehicles, 'on_route', 'delayed').map((item) => item.unitId)).toEqual([1])
    expect(filterVehicles(vehicles, 'not_on_route', 'all').map((item) => item.unitId)).toEqual([3, 4])
    expect(filterVehicles(vehicles, 'all', 'on_time').map((item) => item.unitId)).toEqual([2])
  })
})
