import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { LiveMapContext, type LiveMapState } from '../live/liveMapContext'
import { IncidentsPage } from './IncidentsPage'

const state: LiveMapState = {
  connection: 'live', version: 4, vehicles: [],
  catalog: { schemaVersion: '2', catalogVersion: 'test', stops: [], routes: [{ id: 'route_pattern_demo', officialRouteId: '310', officialRouteName: 'Станция Перово — Метро Щёлковская', officialMatchQuality: 'confirmed', stopIds: [], polyline: [], frequentPoints: [], geometryQuality: 'stops_only', quality: { occurrenceCount: 1, goodGpsOccurrenceCount: 1, bestStopCoverage: 1, maxGpsJumpMeters: 0 } }], occurrences: [], vehicleBindings: [], riskThresholds: { watchDelaySeconds: 180, highDelaySeconds: 420 } },
  predictions: [{ id: 'prediction-1', unitId: 893159, trId: 122048, routePatternId: 'route_pattern_demo', occurrenceId: 'run-1', targetActionItemId: 55, targetStop: { id: 'stop-1', address: 'Тестовая остановка' }, predictionTime: '2026-01-06T10:00:00Z', targetPlannedAt: '2026-01-06T10:12:00Z', currentDelaySeconds: 90, predictedDelaySeconds: 240 }],
  incidents: [{ id: 'incident-1', eventType: 'new', status: 'active', unitId: 893159, trId: 122048, routePatternId: 'route_pattern_demo', occurrenceId: 'run-1', predictionId: 'prediction-1', targetActionItemId: 55, targetStop: { id: 'stop-1', address: 'Тестовая остановка' }, predictedDelaySeconds: 240, firstPredictedDelaySeconds: 240, predictionTime: '2026-01-06T10:00:00Z', targetPlannedAt: '2026-01-06T10:12:00Z', createdAt: '2026-01-06T10:00:01Z', updatedAt: '2026-01-06T10:00:02Z', reasonCode: 'door_hold_delay', reason: 'Длительная стоянка с открытыми дверями.', evidence: { door_open_duration_s: 142, speed_mean_5m_kmh: 1.2, custom_demo_metric: 7 }, scenarioId: 'door-test' }],
}

afterEach(cleanup)

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/incidents']}>
      <LiveMapContext.Provider value={state}>
        <Routes>
          <Route path="/incidents" element={<IncidentsPage />} />
          <Route path="/incidents/:incidentId" element={<IncidentsPage />} />
        </Routes>
      </LiveMapContext.Provider>
    </MemoryRouter>,
  )
}

describe('IncidentsPage', () => {
  it('разделяет текущие и ожидающие инциденты', () => {
    renderPage()
    expect(screen.getAllByText('Маршрут №310').length).toBeGreaterThan(0)
    expect(screen.getByText('Тестовая остановка')).toBeInTheDocument()
    expect(screen.getByText('+4:00')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Текущие/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Ожидают результата/ })).toBeInTheDocument()
    expect(screen.getByText('Средняя · WATCH')).toBeInTheDocument()
    expect(screen.getByText('Длительная стоянка с открытыми дверями.')).toBeInTheDocument()
  })

  it('фильтрует очередь по поиску и критичности', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByLabelText('Поиск инцидентов'), 'несуществующий')
    expect(screen.queryByText('Тестовая остановка')).not.toBeInTheDocument()
    await user.clear(screen.getByLabelText('Поиск инцидентов'))
    expect(screen.getByText('Тестовая остановка')).toBeInTheDocument()
  })

  it('показывает понятные признаки и тестовую пометку в карточке', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.click(screen.getByText('ТС 893159'))
    expect(screen.getByRole('heading', { name: 'Возможная причина' })).toBeInTheDocument()
    expect(screen.getByText('Тестовый сценарий')).toBeInTheDocument()
    expect(screen.getByText('Двери открыты')).toBeInTheDocument()
    expect(screen.getByText('2 мин 22 с')).toBeInTheDocument()
    expect(screen.getByText('Дополнительный признак (custom_demo_metric)')).toBeInTheDocument()
    expect(screen.queryByText('door_hold_delay')).not.toBeInTheDocument()
  })

  it('показывает нейтральный текст при отсутствии причины', () => {
    render(<MemoryRouter><LiveMapContext.Provider value={{ ...state, incidents: [{ ...state.incidents[0], reason: undefined, evidence: undefined, scenarioId: undefined }] }}><IncidentsPage /></LiveMapContext.Provider></MemoryRouter>)
    expect(screen.getByText('Причина не определена')).toBeInTheDocument()
  })
})
