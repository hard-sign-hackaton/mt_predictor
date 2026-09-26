import { useEffect, useMemo, useReducer, type ReactNode } from 'react'
import type { DashboardInit, LiveEvent, MapSnapshot } from '../api/types'
import { LiveMapContext, type ConnectionState, type LiveMapState } from './liveMapContext'

type Action =
  | { type: 'catalog'; catalog: DashboardInit }
  | { type: 'snapshot'; snapshot: MapSnapshot }
  | { type: 'vehicle'; event: Extract<LiveEvent, { type: 'vehicle_updated' }> }
  | { type: 'connection'; connection: ConnectionState; error?: string }

const initialState: LiveMapState = { vehicles: [], version: 0, connection: 'loading' }

function reducer(state: LiveMapState, action: Action): LiveMapState {
  switch (action.type) {
    case 'catalog':
      return { ...state, catalog: action.catalog }
    case 'snapshot':
      return { ...state, vehicles: action.snapshot.vehicles, version: action.snapshot.version }
    case 'vehicle': {
      if (action.event.sequence <= state.version) return state
      const next = state.vehicles.filter((vehicle) => vehicle.unitId !== action.event.vehicle.unitId)
      next.push(action.event.vehicle)
      next.sort((left, right) => left.unitId - right.unitId)
      return { ...state, vehicles: next, version: action.event.sequence }
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

  useEffect(() => {
    let disposed = false
    let stream: EventSource | undefined

    async function connect() {
      try {
        const [catalog, snapshot] = await Promise.all([
          getJSON<DashboardInit>('/api/v1/map/init'),
          getJSON<MapSnapshot>('/api/v1/map/snapshot'),
        ])
        if (disposed) return
        dispatch({ type: 'catalog', catalog })
        dispatch({ type: 'snapshot', snapshot })

        stream = new EventSource('/api/v1/map/events')
        stream.onopen = () => dispatch({ type: 'connection', connection: 'live' })
        stream.onerror = () => dispatch({ type: 'connection', connection: 'reconnecting', error: 'Поток недоступен, выполняется переподключение' })
        stream.addEventListener('snapshot', (message) => {
          dispatch({ type: 'snapshot', snapshot: JSON.parse(message.data) as MapSnapshot })
        })
        stream.addEventListener('vehicle_updated', (message) => {
          dispatch({ type: 'vehicle', event: JSON.parse(message.data) as Extract<LiveEvent, { type: 'vehicle_updated' }> })
        })
      } catch (error) {
        if (!disposed) dispatch({ type: 'connection', connection: 'error', error: error instanceof Error ? error.message : 'Не удалось подключиться к backend' })
      }
    }

    void connect()
    return () => {
      disposed = true
      stream?.close()
    }
  }, [])

  const value = useMemo(() => state, [state])
  return <LiveMapContext.Provider value={value}>{children}</LiveMapContext.Provider>
}
