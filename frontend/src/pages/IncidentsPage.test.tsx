import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { LiveMapContext, type LiveMapState } from '../live/liveMapContext'
import { IncidentsPage } from './IncidentsPage'

const state: LiveMapState = {
  connection: 'live', version: 4, vehicles: [],
  catalog: { schemaVersion: '1', catalogVersion: 'test', stops: [], routes: [], occurrences: [], vehicleBindings: [], riskThresholds: { watchDelaySeconds: 180, highDelaySeconds: 420 } },
  predictions: [{ id: 'prediction-1', unitId: 893159, trId: 122048, routePatternId: 'route_pattern_demo', targetActionItemId: 55, targetStop: { id: 'stop-1', address: 'Тестовая остановка' }, predictionTime: '2026-01-06T10:00:00Z', targetPlannedAt: '2026-01-06T10:12:00Z', currentDelaySeconds: 90, predictedDelaySeconds: 240, reasonCode: 'door_hold_delay', reason: 'Двери оставались открыты дольше порога.' }],
  incidents: [{ id: 'incident-1', eventType: 'new', unitId: 893159, trId: 122048, routePatternId: 'route_pattern_demo', predictionId: 'prediction-1', targetActionItemId: 55, targetStop: { id: 'stop-1', address: 'Тестовая остановка' }, predictedDelaySeconds: 240, reasonCode: 'door_hold_delay', reason: 'Двери оставались открыты дольше порога.', evidence: { door_open_duration_s: 142 }, scenarioId: 'door-hold-01', predictionTime: '2026-01-06T10:00:00Z', createdAt: '2026-01-06T10:00:01Z', updatedAt: '2026-01-06T10:00:02Z' }],
}

afterEach(cleanup)

function renderPage() {
  return render(<LiveMapContext.Provider value={state}><IncidentsPage /></LiveMapContext.Provider>)
}

describe('IncidentsPage', () => {
  it('показывает только факты текущего контракта и вычисляет горизонт', () => {
    renderPage()
    expect(screen.getByText('ТС 893159')).toBeInTheDocument()
    expect(screen.getByText('Тестовая остановка')).toBeInTheDocument()
    expect(screen.getByText('+4:00')).toBeInTheDocument()
    expect(screen.getByText('12 мин')).toBeInTheDocument()
    expect(screen.getAllByText('Средняя · WATCH')).toHaveLength(2)
    expect(screen.getByText('Двери оставались открыты дольше порога.')).toBeInTheDocument()
    expect(screen.getByText('Тестовый сценарий: door-hold-01')).toBeInTheDocument()
    expect(screen.getByText('Двери открыты, с: 142')).toBeInTheDocument()
    expect(screen.queryByText(/confidence|ответственный/i)).not.toBeInTheDocument()
  })

  it('фильтрует очередь по поиску и критичности', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByLabelText('Поиск инцидентов'), 'несуществующий')
    expect(screen.getByText('Нет инцидентов по выбранным фильтрам')).toBeInTheDocument()
    await user.click(screen.getByText('Сбросить'))
    expect(screen.getByText('ТС 893159')).toBeInTheDocument()
  })
})
