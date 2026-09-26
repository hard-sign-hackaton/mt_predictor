import { render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { DashboardInit, VehicleState } from '../api/types'
import { MapCameraController } from './MapCameraController'

const map = {
  fitBounds: vi.fn(),
  flyTo: vi.fn(),
  getZoom: vi.fn(() => 10),
}

vi.mock('react-leaflet', () => ({ useMap: () => map }))

const catalog = {
  routes: [{
    id: 'route-1', stopIds: [], frequentPoints: [], geometryQuality: 'gps_repeated',
    polyline: [{ lon: 37.6, lat: 55.7 }, { lon: 37.7, lat: 55.8 }],
    quality: { occurrenceCount: 1, goodGpsOccurrenceCount: 1, bestStopCoverage: 1, maxGpsJumpMeters: 10 },
  }],
  stops: [], occurrences: [], vehicleBindings: [],
  schemaVersion: '1', catalogVersion: 'test',
  riskThresholds: { watchDelaySeconds: 180, highDelaySeconds: 420 },
} satisfies DashboardInit

function vehicle(lon: number): VehicleState {
  return {
    unitId: 100, position: { lon, lat: 55.75 }, speedKmh: 20, headingDegrees: 90,
    eventTime: '2026-09-27T00:00:00Z', receivedAt: '2026-09-27T00:00:00Z',
    historical: false, freshness: 'live', matchStatus: 'matched_spatial',
  }
}

describe('MapCameraController', () => {
  beforeEach(() => vi.clearAllMocks())

  it('не сбрасывает общий масштаб при потоковом обновлении транспорта', () => {
    const view = render(<MapCameraController catalog={catalog} vehicles={[vehicle(37.61)]} />)
    expect(map.fitBounds).toHaveBeenCalledTimes(1)

    view.rerender(<MapCameraController catalog={catalog} vehicles={[vehicle(37.62)]} />)
    expect(map.fitBounds).toHaveBeenCalledTimes(1)
    expect(map.flyTo).not.toHaveBeenCalled()
  })

  it('центрирует выбранное ТС один раз и не преследует каждый новый пакет', () => {
    const view = render(<MapCameraController catalog={catalog} vehicles={[vehicle(37.61)]} />)
    view.rerender(<MapCameraController catalog={catalog} vehicles={[vehicle(37.61)]} selectedVehicleId={100} />)
    expect(map.flyTo).toHaveBeenCalledTimes(1)
    expect(map.flyTo).toHaveBeenCalledWith([55.75, 37.61], 14)

    view.rerender(<MapCameraController catalog={catalog} vehicles={[vehicle(37.64)]} selectedVehicleId={100} />)
    expect(map.flyTo).toHaveBeenCalledTimes(1)
  })

  it('перестраивает границы при явном выборе маршрута', () => {
    const view = render(<MapCameraController catalog={catalog} vehicles={[vehicle(37.61)]} />)
    view.rerender(<MapCameraController catalog={catalog} vehicles={[vehicle(37.61)]} selectedRouteId="route-1" />)
    expect(map.fitBounds).toHaveBeenCalledTimes(2)
  })
})
