import { describe, expect, it } from 'vitest'
import { cloneInitialState } from '../mockData'
import { dashboardReducer } from '../store'

describe('dashboard reducer', () => {
  it('journals incident status changes', () => {
    const state = cloneInitialState()
    const next = dashboardReducer(state, { type: 'SET_INCIDENT_STATUS', incidentId: 'INC-204', status: 'in_progress', owner: 'Test', comment: 'Принято', now: '2026-09-26T10:00:00+05:00' })
    expect(next.incidents.find((item) => item.id === 'INC-204')?.status).toBe('in_progress')
    expect(next.actions[0].type).toBe('status_change')
  })

  it('does not tick when simulation is paused', () => {
    const state = { ...cloneInitialState(), simulationPaused: true }
    expect(dashboardReducer(state, { type: 'TICK', now: '2026-09-26T10:00:00+05:00' })).toBe(state)
  })

  it('restores deterministic data on reset', () => {
    const state = { ...cloneInitialState(), incidents: [] }
    expect(dashboardReducer(state, { type: 'RESET' }).incidents).toHaveLength(3)
  })
})
