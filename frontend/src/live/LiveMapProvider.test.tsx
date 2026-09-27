import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DashboardInit, DashboardSnapshot, Incident, LiveEvent } from '../api/types'
import { LiveMapProvider } from './LiveMapProvider'
import { useLiveMap } from './liveMapContext'

const catalog: DashboardInit = {
  schemaVersion: '1', catalogVersion: 'test', stops: [], routes: [], occurrences: [], vehicleBindings: [],
  riskThresholds: { watchDelaySeconds: 180, highDelaySeconds: 420 },
}

const initialSnapshot: DashboardSnapshot = {
  version: 10, createdAt: '2026-01-06T10:00:00Z', predictions: [], incidents: [],
  vehicles: [{ unitId: 7, speedKmh: 10, headingDegrees: 90, eventTime: '2026-01-06T10:00:00Z', receivedAt: '2026-01-06T10:00:01Z', historical: false, freshness: 'live', matchStatus: 'matched' }],
}

const incident: Incident = {
  id: 'incident-7-99', eventType: 'new', status: 'active', unitId: 7, trId: 42, routePatternId: 'route_pattern_test', occurrenceId: 'run-1',
  predictionId: 'prediction-7-99', targetActionItemId: 99, targetStop: { id: 'stop-99', address: 'Тестовая остановка' },
  predictedDelaySeconds: 240, firstPredictedDelaySeconds: 240, predictionTime: '2026-01-06T10:01:00Z', targetPlannedAt: '2026-01-06T10:13:00Z', createdAt: '2026-01-06T10:01:01Z', updatedAt: '2026-01-06T10:01:01Z',
}

class FakeEventSource {
  static current?: FakeEventSource
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>()

  constructor(url: string) {
    void url
    FakeEventSource.current = this
  }
  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    const callback = listener as (event: MessageEvent<string>) => void
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), callback])
  }
  emit(type: string, value: unknown) {
    for (const listener of this.listeners.get(type) ?? []) listener({ data: JSON.stringify(value) } as MessageEvent<string>)
  }
  close() {}
}

function Probe() {
  const state = useLiveMap()
  return <output data-testid="state">{JSON.stringify(state)}</output>
}

function jsonResponse(value: unknown) {
  return { ok: true, json: async () => value } as Response
}

type EventPayload<T> = T extends LiveEvent ? Omit<T, 'eventId' | 'occurredAt'> : never

function event(value: EventPayload<LiveEvent>): LiveEvent {
  return { eventId: `event-${value.sequence}`, occurredAt: '2026-01-06T10:01:00Z', ...value } as LiveEvent
}

beforeEach(() => {
  FakeEventSource.current = undefined
  vi.stubGlobal('EventSource', FakeEventSource)
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('LiveMapProvider', () => {
  it('гидратирует snapshot, применяет события и удаляет закрытый инцидент', async () => {
    vi.stubGlobal('fetch', vi.fn((path: string) => Promise.resolve(jsonResponse(path.includes('/map/init') ? catalog : initialSnapshot))))
    render(<LiveMapProvider><Probe /></LiveMapProvider>)

    await waitFor(() => expect(screen.getByTestId('state')).toHaveTextContent('"version":10'))
    const stream = FakeEventSource.current!
    act(() => stream.emit('vehicle_updated', event({ type: 'vehicle_updated', sequence: 11, vehicle: { ...initialSnapshot.vehicles[0], speedKmh: 25 } })))
    act(() => stream.emit('prediction_updated', event({ type: 'prediction_updated', sequence: 12, prediction: { id: 'prediction-7-99', unitId: 7, trId: 42, routePatternId: 'route_pattern_test', occurrenceId: 'run-1', targetActionItemId: 99, targetStop: incident.targetStop, predictionTime: incident.predictionTime, targetPlannedAt: '2026-01-06T10:13:00Z', predictedDelaySeconds: 240 } })))
    act(() => stream.emit('incident_updated', event({ type: 'incident_updated', sequence: 13, incident })))
    await waitFor(() => expect(screen.getByTestId('state')).toHaveTextContent('"version":13'))
    expect(screen.getByTestId('state')).toHaveTextContent('"speedKmh":25')
    expect(screen.getByTestId('state')).toHaveTextContent('incident-7-99')

    act(() => stream.emit('incident_updated', event({ type: 'incident_updated', sequence: 14, incident: { ...incident, eventType: 'closed' } })))
    await waitFor(() => expect(screen.getByTestId('state')).toHaveTextContent('"version":14'))
    expect(screen.getByTestId('state')).not.toHaveTextContent('incident-7-99')
  })

  it('повторно загружает snapshot при разрыве sequence', async () => {
    const recovered = { ...initialSnapshot, version: 20, vehicles: [{ ...initialSnapshot.vehicles[0], speedKmh: 33 }] }
    const dashboardSnapshots = [initialSnapshot, recovered]
    const fetchMock = vi.fn((path: string) => Promise.resolve(jsonResponse(path.includes('/map/init') ? catalog : dashboardSnapshots.shift())))
    vi.stubGlobal('fetch', fetchMock)
    render(<LiveMapProvider><Probe /></LiveMapProvider>)
    await waitFor(() => expect(screen.getByTestId('state')).toHaveTextContent('"version":10'))

    act(() => FakeEventSource.current!.emit('vehicle_updated', event({ type: 'vehicle_updated', sequence: 15, vehicle: initialSnapshot.vehicles[0] })))
    await waitFor(() => expect(screen.getByTestId('state')).toHaveTextContent('"version":20'))
    expect(screen.getByTestId('state')).toHaveTextContent('"speedKmh":33')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/dashboard/snapshot')
  })
})
