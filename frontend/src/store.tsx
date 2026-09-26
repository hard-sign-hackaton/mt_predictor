import { createContext, type ReactNode, useContext, useEffect, useMemo, useReducer } from 'react'
import { cloneInitialState } from './mockData'
/* eslint-disable react-refresh/only-export-components */
import type { DashboardState, IncidentStatus, OperatorAction, SystemSnapshot, WhatIfResult } from './types'

const STORAGE_KEY = 'mt-predictor-dashboard-v1'

export type DashboardAction =
  | { type: 'TICK'; now: string }
  | { type: 'TOGGLE_SIMULATION' }
  | { type: 'SET_INCIDENT_STATUS'; incidentId: string; status: IncidentStatus; owner: string; comment: string; now: string }
  | { type: 'ADD_ACTION'; action: OperatorAction }
  | { type: 'UPDATE_ACTION_STATUS'; actionId: string; status: OperatorAction['status'] }
  | { type: 'REGISTER_WHAT_IF'; result: WhatIfResult; action: OperatorAction }
  | { type: 'SET_SYSTEM_DEMO'; value: 'normal' | 'degraded' | 'ml_unavailable' | 'map_unavailable' | 'empty' }
  | { type: 'RESET' }

export function dashboardReducer(state: DashboardState, action: DashboardAction): DashboardState {
  switch (action.type) {
    case 'TICK': {
      if (state.simulationPaused) return state
      const tick = state.tick + 1
      const delta = tick % 2 === 0 ? 10 : -5
      return {
        ...state,
        tick,
        system: { ...state.system, snapshotAt: action.now, lastPacketAt: action.now },
        vehicles: state.vehicles.map((vehicle, index) => {
          if (vehicle.telemetry !== 'live') return vehicle
          return {
            ...vehicle,
            position: {
              x: Math.min(94, vehicle.position.x + (index % 2 === 0 ? 0.7 : 0.45)),
              y: Math.max(8, vehicle.position.y - (index % 2 === 0 ? 0.35 : -0.2)),
            },
            lastTelemetryAt: action.now,
            predictedDelaySeconds: vehicle.id === '4712' ? Math.max(450, Math.min(570, vehicle.predictedDelaySeconds + delta)) : vehicle.predictedDelaySeconds,
          }
        }),
        incidents: state.incidents.map((incident) => incident.id === 'INC-204'
          ? { ...incident, predictedDelaySeconds: Math.max(450, Math.min(570, incident.predictedDelaySeconds + delta)), updatedAt: action.now }
          : incident),
      }
    }
    case 'TOGGLE_SIMULATION':
      return { ...state, simulationPaused: !state.simulationPaused }
    case 'SET_INCIDENT_STATUS': {
      const journalAction: OperatorAction = {
        id: `ACT-${Date.now()}`,
        incidentId: action.incidentId,
        routeId: state.incidents.find((item) => item.id === action.incidentId)?.routeId ?? '',
        vehicleId: state.incidents.find((item) => item.id === action.incidentId)?.vehicleId ?? '',
        type: 'status_change',
        summary: `Статус изменён на ${action.status}. ${action.comment}`.trim(),
        author: action.owner,
        createdAt: action.now,
        status: 'registered',
        isMock: true,
      }
      return {
        ...state,
        incidents: state.incidents.map((incident) => incident.id === action.incidentId ? { ...incident, status: action.status, owner: action.owner, comment: action.comment, updatedAt: action.now } : incident),
        actions: [journalAction, ...state.actions],
      }
    }
    case 'ADD_ACTION':
      return { ...state, actions: [action.action, ...state.actions] }
    case 'UPDATE_ACTION_STATUS':
      return { ...state, actions: state.actions.map((item) => item.id === action.actionId ? { ...item, status: action.status } : item) }
    case 'REGISTER_WHAT_IF':
      return { ...state, actions: [action.action, ...state.actions] }
    case 'SET_SYSTEM_DEMO': {
      if (action.value === 'empty') return { ...state, incidents: [], vehicles: [] }
      const system: SystemSnapshot = { ...state.system, ingestion: 'ok', websocket: 'ok', backend: 'ok', ml: 'ok', map: 'ok' }
      if (action.value === 'degraded') {
        system.ingestion = 'degraded'
        system.websocket = 'degraded'
      }
      if (action.value === 'ml_unavailable') system.ml = 'unavailable'
      if (action.value === 'map_unavailable') system.map = 'unavailable'
      return { ...state, system }
    }
    case 'RESET':
      return cloneInitialState()
  }
}

function loadState(): DashboardState {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    return saved ? JSON.parse(saved) as DashboardState : cloneInitialState()
  } catch {
    return cloneInitialState()
  }
}

interface DashboardContextValue {
  state: DashboardState
  dispatch: React.Dispatch<DashboardAction>
}

const DashboardContext = createContext<DashboardContextValue | null>(null)

export function DashboardProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(dashboardReducer, undefined, loadState)

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  }, [state])

  useEffect(() => {
    const interval = window.setInterval(() => dispatch({ type: 'TICK', now: new Date().toISOString() }), 5000)
    return () => window.clearInterval(interval)
  }, [])

  const value = useMemo(() => ({ state, dispatch }), [state])
  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>
}

export function useDashboard() {
  const context = useContext(DashboardContext)
  if (!context) throw new Error('useDashboard must be used within DashboardProvider')
  return context
}
