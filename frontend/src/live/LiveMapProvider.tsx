import { useEffect, useMemo, useReducer, useRef, type ReactNode } from 'react'
import type { DashboardInit, DashboardSnapshot, LiveEvent } from '../api/types'
import { LiveMapContext, type ConnectionState, type LiveMapState } from './liveMapContext'

type Action =
  | { type: 'catalog'; catalog: DashboardInit }
  | { type: 'snapshot'; snapshot: DashboardSnapshot }
  | { type: 'vehicle'; event: Extract<LiveEvent, { type: 'vehicle_updated' }> }
  | { type: 'prediction'; event: Extract<LiveEvent, { type: 'prediction_updated' }> }
  | { type: 'incident'; event: Extract<LiveEvent, { type: 'incident_updated' }> }
  | { type: 'events'; events: LiveEvent[] }
  | { type: 'connection'; connection: ConnectionState; error?: string }

const initialState: LiveMapState = { vehicles: [], predictions: [], incidents: [], version: 0, connection: 'loading' }

function reducer(state: LiveMapState, action: Action): LiveMapState {
  if (action.type === 'events') {
    return action.events.reduce((next, event) => {
      if (event.type === 'vehicle_updated') return reducer(next, { type: 'vehicle', event })
      if (event.type === 'prediction_updated') return reducer(next, { type: 'prediction', event })
      if (event.type === 'incident_updated') return reducer(next, { type: 'incident', event })
      return next
    }, state)
  }

  switch (action.type) {
    case 'catalog':
      return { ...state, catalog: action.catalog }
    case 'snapshot':
      return { ...state, vehicles: action.snapshot.vehicles, predictions: action.snapshot.predictions, incidents: action.snapshot.incidents, version: action.snapshot.version }
    case 'vehicle': {
      if (action.event.sequence <= state.version) return state
      const next = state.vehicles.filter((vehicle) => vehicle.unitId !== action.event.vehicle.unitId)
      next.push(action.event.vehicle)
      next.sort((left, right) => left.unitId - right.unitId)
      return { ...state, vehicles: next, version: action.event.sequence }
    }
    case 'prediction': {
      if (action.event.sequence <= state.version) return state
      const prediction = action.event.prediction
      const next = state.predictions.filter((item) => item.unitId !== prediction.unitId || item.targetActionItemId !== prediction.targetActionItemId)
      next.push(prediction)
      return { ...state, predictions: next, version: action.event.sequence }
    }
    case 'incident': {
      if (action.event.sequence <= state.version) return state
      const incident = action.event.incident
      const next = state.incidents.filter((item) => item.id !== incident.id)
      if (incident.eventType !== 'closed') next.push(incident)
      return { ...state, incidents: next, version: action.event.sequence }
    }
    case 'connection':
      return { ...state, connection: action.connection, error: action.error }
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path)
  if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`)
  return response.json() as Promise<T>
}

export function LiveMapProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState)
  const versionRef = useRef(0)

  useEffect(() => {
    let disposed = false
    let stream: EventSource | undefined
    let flushTimer: ReturnType<typeof setTimeout> | undefined
    let pendingEvents: LiveEvent[] = []

    function flushEvents() {
      flushTimer = undefined
      if (!pendingEvents.length || disposed) return
      const events = pendingEvents
      pendingEvents = []
      dispatch({ type: 'events', events })
    }

    function queueEvent(event: LiveEvent) {
      pendingEvents.push(event)
      // Replay 50x может присылать сотни пакетов в секунду. Один render на
      // короткий пакет сохраняет live-ощущение, но не блокирует навигацию и фильтры.
      flushTimer ??= setTimeout(flushEvents, 100)
    }

    async function loadSnapshot() {
      const snapshot = await getJSON<DashboardSnapshot>('/api/v1/dashboard/snapshot')
      versionRef.current = snapshot.version
      pendingEvents = []
      dispatch({ type: 'snapshot', snapshot })
    }

    async function applyEvent(event: LiveEvent) {
      if (event.sequence > versionRef.current + 1) {
        await loadSnapshot()
        return
      }
      if (event.sequence <= versionRef.current) return
      versionRef.current = event.sequence
      queueEvent(event)
    }

    async function connect() {
      try {
        const [catalog, snapshot] = await Promise.all([
          getJSON<DashboardInit>('/api/v1/map/init'),
          getJSON<DashboardSnapshot>('/api/v1/dashboard/snapshot'),
        ])
        if (disposed) return
        dispatch({ type: 'catalog', catalog })
        versionRef.current = snapshot.version
        dispatch({ type: 'snapshot', snapshot })

        stream = new EventSource('/api/v1/map/events')
        stream.onopen = () => dispatch({ type: 'connection', connection: 'live' })
        stream.onerror = () => dispatch({ type: 'connection', connection: 'reconnecting', error: 'Поток недоступен, выполняется переподключение' })
        stream.addEventListener('snapshot', (message) => {
          const next = JSON.parse(message.data) as DashboardSnapshot
          versionRef.current = next.version
          pendingEvents = []
          dispatch({ type: 'snapshot', snapshot: next })
        })
        stream.addEventListener('vehicle_updated', (message) => {
          void applyEvent(JSON.parse(message.data) as LiveEvent)
        })
        stream.addEventListener('prediction_updated', (message) => {
          void applyEvent(JSON.parse(message.data) as LiveEvent)
        })
        stream.addEventListener('incident_updated', (message) => {
          void applyEvent(JSON.parse(message.data) as LiveEvent)
        })
      } catch (error) {
        if (!disposed) dispatch({ type: 'connection', connection: 'error', error: error instanceof Error ? error.message : 'Не удалось подключиться к backend' })
      }
    }

    void connect()
    return () => {
      disposed = true
      if (flushTimer) clearTimeout(flushTimer)
      stream?.close()
    }
  }, [])

  const value = useMemo(() => state, [state])
  return <LiveMapContext.Provider value={value}>{children}</LiveMapContext.Provider>
}
