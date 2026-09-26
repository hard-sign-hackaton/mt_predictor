import { createContext, useContext } from 'react'
import type { DashboardInit, VehicleState } from '../api/types'

export type ConnectionState = 'loading' | 'live' | 'reconnecting' | 'error'

export interface LiveMapState {
  catalog?: DashboardInit
  vehicles: VehicleState[]
  version: number
  connection: ConnectionState
  error?: string
}

export const LiveMapContext = createContext<LiveMapState | null>(null)

export function useLiveMap() {
  const context = useContext(LiveMapContext)
  if (!context) throw new Error('useLiveMap должен использоваться внутри LiveMapProvider')
  return context
}
