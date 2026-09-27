import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { RouteOccurrence, VehicleState, WhatIfMode, WhatIfReport } from '../api/types'
import { WhatIfPanel } from './WhatIfPanel'

const vehicle: VehicleState = {
  unitId: 664030, matchStatus: 'no_schedule', freshness: 'live', speedKmh: 0, headingDegrees: 0,
  eventTime: '2026-01-06T10:00:00Z', receivedAt: '2026-01-06T10:00:00Z', historical: false,
  position: { lon: 37.617, lat: 55.755 },
}

function occurrence(id: string, calls: number, pattern = 'route_pattern_test'): RouteOccurrence {
  return {
    id, trId: 130387, routePatternId: pattern, startsAt: '2026-01-06T10:00:00Z', endsAt: '2026-01-06T12:00:00Z',
    calls: Array.from({ length: calls }, (_, index) => ({
      actionItemId: index + 1, stopId: `s${index % 3}`, plannedAt: new Date(Date.UTC(2026, 0, 6, 10, index)).toISOString(),
    })),
  }
}

function whatIf(mode: WhatIfMode, overrides: Partial<WhatIfReport> = {}): WhatIfReport {
  return {
    mode, candidate: { unitId: vehicle.unitId, matchStatus: 'no_schedule', freshness: 'live', eventTime: vehicle.eventTime },
    target: { occurrenceId: 'occ_a', trId: 130387, routePatternId: 'route_pattern_test', joinCallIndex: 2, joinActionItemId: 3, joinStopId: 's2', joinStopAddress: 'Остановка 2', joinPlannedAt: '2026-01-06T10:02:00Z', totalCalls: 4, uniqueStops: 3 },
    deadhead: { verdict: 'feasible', meters: 1840, seconds: 221, slackSeconds: 340, assumedEmptySpeedKmh: 30 },
    conflict: { detected: false },
    partition: { callsVehicleOne: 2, callsVehicleTwo: 2, stopsServedByOne: 1, stopsServedByTwo: 2, fullRunSeconds: 180, longestVehicleRunSeconds: 120 },
    serviceInterval: { meanHeadwayBeforeSeconds: 60, meanHeadwayAfterSeconds: 60, improvedStops: [] },
    scheduleReliefSeconds: null, unavailableReason: 'no_capacity_model',
    assumptions: ['скорость перегона без пассажиров 30 км/ч'], generatedAt: '2026-01-06T10:00:00Z',
    ...overrides,
  }
}

let respond: (mode: WhatIfMode) => WhatIfReport
let requested: string[]

beforeEach(() => {
  requested = []
  respond = (mode) => whatIf(mode)
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    requested.push(String(input))
    const mode = new URL(String(input), 'http://localhost').searchParams.get('mode') as WhatIfMode
    return { ok: true, status: 200, json: async () => respond(mode) } as unknown as Response
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderPanel(occurrences = [occurrence('occ_a', 4)], hasSchedule = false) {
  return render(<WhatIfPanel vehicle={vehicle} vehicleName="ТС 664030" occurrences={occurrences} hasSchedule={hasSchedule} />)
}

describe('WhatIfPanel', () => {
  it('запрашивает сценарий для выбранного ТС и рейса', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('Успевает')).toBeInTheDocument())
    expect(requested).toHaveLength(1)
    const url = new URL(requested[0], 'http://localhost')
    expect(url.pathname).toBe('/api/v1/whatif')
    expect(url.searchParams.get('unitId')).toBe('664030')
    expect(url.searchParams.get('occurrenceId')).toBe('occ_a')
    expect(url.searchParams.get('mode')).toBe('relieve')
    // Граница по умолчанию — середина рейса из четырёх call.
    expect(url.searchParams.get('joinCallIndex')).toBe('2')
  })

  it('показывает перегон, запас и раздел рейса', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('Успевает')).toBeInTheDocument())
    expect(screen.getByText('1.8 км')).toBeInTheDocument()
    expect(screen.getByText('3 мин 41 с')).toBeInTheDocument()
    expect(screen.getByText('30 км/ч (допущение)')).toBeInTheDocument()
    expect(screen.getByText('Остановка 2')).toBeInTheDocument()
    expect(screen.getByText('3 мин')).toBeInTheDocument()
    expect(screen.getByText('2 мин')).toBeInTheDocument()
  })

  it('объясняет, что интервал в режиме разделения не меняется', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('Успевает')).toBeInTheDocument())
    expect(screen.getByText(/Интервал обслуживания не меняется/)).toBeInTheDocument()
  })

  it('никогда не показывает сокращение задержки по графику', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('Успевает')).toBeInTheDocument())
    expect(screen.getByText(/ровно одно ТС на маршрут/)).toBeInTheDocument()
    expect(screen.queryByText(/сокращение задержки.*сек/i)).not.toBeInTheDocument()
  })

  it('переключение режима перезапрашивает сценарий', async () => {
    const user = userEvent.setup()
    renderPanel()
    await waitFor(() => expect(screen.getByText('Успевает')).toBeInTheDocument())
    await user.selectOptions(screen.getByLabelText('Режим сценария'), 'duplicate')
    await waitFor(() => expect(screen.getByText(/Дублирование рейса/)).toBeInTheDocument())
    expect(new URL(requested.at(-1)!, 'http://localhost').searchParams.get('mode')).toBe('duplicate')
  })

  it('в режиме дублирования показывает таблицу сокращения интервала', async () => {
    respond = (mode) => whatIf(mode, {
      serviceInterval: {
        meanHeadwayBeforeSeconds: 1800, meanHeadwayAfterSeconds: 600,
        improvedStops: [{ stopId: 's2', address: 'Остановка 2', visitsBefore: 2, visitsAfter: 4, headwayBeforeSeconds: 1800, headwayAfterSeconds: 600, reduction: 2 / 3 }],
      },
    })
    const user = userEvent.setup()
    renderPanel()
    await user.selectOptions(screen.getByLabelText('Режим сценария'), 'duplicate')
    await waitFor(() => expect(screen.getByText('−67%')).toBeInTheDocument())
    const table = within(screen.getByRole('table'))
    expect(table.getByText('Остановка 2')).toBeInTheDocument()
    expect(table.getByText('30 мин')).toBeInTheDocument()
    expect(table.getByText('10 мин')).toBeInTheDocument()
    expect(screen.queryByText(/Интервал обслуживания не меняется/)).not.toBeInTheDocument()
  })

  it('передаёт границу разбиения при перетаскивании ползунка', async () => {
    renderPanel()
    await waitFor(() => expect(requested).toHaveLength(1))
    fireEvent.change(screen.getByLabelText('Граница разбиения рейса'), { target: { value: '0' } })
    await waitFor(() => expect(new URL(requested.at(-1)!, 'http://localhost').searchParams.get('joinCallIndex')).toBe('0'))
  })

  it('показывает конфликт с собственным расписанием кандидата', async () => {
    respond = (mode) => whatIf(mode, {
      conflict: { detected: true, occurrenceId: 'occ_other', trId: 130387, validFrom: '2026-01-06T10:00:00Z', validTo: '2026-01-06T12:00:00Z' },
    })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/Конфликт/)).toBeInTheDocument())
    expect(screen.getByText(/occ_other/)).toBeInTheDocument()
  })

  it('отсутствие телеметрии показывает как «нет данных», а не как ноль', async () => {
    respond = (mode) => whatIf(mode, {
      deadhead: { verdict: 'unknown_vehicle', assumedEmptySpeedKmh: 30, unavailableReason: 'unknown_vehicle' },
    })
    renderPanel()
    await waitFor(() => expect(screen.getByText('ТС ещё не в эфире')).toBeInTheDocument())
    expect(screen.getAllByText('нет данных').length).toBeGreaterThan(0)
    expect(screen.getByText(/ещё не появилось в потоке телеметрии/)).toBeInTheDocument()
  })

  // Регрессия: историческое расписание давало запас −6329 ч, который выглядел
  // как обычное число. Теперь прошедший рейс назван прямо, а запас не показан.
  it('прошедший рейс показывает «нет данных» вместо абсурдного запаса', async () => {
    respond = (mode) => whatIf(mode, {
      deadhead: {
        verdict: 'run_in_past', assumedEmptySpeedKmh: 30, unavailableReason: 'run_in_past',
        meters: 2496, seconds: 299.5,
      },
    })
    renderPanel()
    await waitFor(() => expect(screen.getByText('Рейс уже прошёл')).toBeInTheDocument())
    expect(screen.getByText(/граница разбиения прошла/)).toBeInTheDocument()
    // Расстояние остаётся фактом, запас — нет.
    expect(screen.getByText('2.5 км')).toBeInTheDocument()
    expect(screen.getByText('5 мин')).toBeInTheDocument()
    expect(screen.getAllByText('нет данных').length).toBeGreaterThan(0)
  })

  it('объясняет, почему ТС без расписания подходит на роль кандидата', async () => {
    renderPanel([occurrence('occ_a', 4)], false)
    await waitFor(() => expect(screen.getByText(/нет опубликованного расписания/)).toBeInTheDocument())
  })

  it('не показывает подсказку о кандидате, если у ТС есть расписание', async () => {
    renderPanel([occurrence('occ_a', 4)], true)
    await waitFor(() => expect(screen.getByText('Успевает')).toBeInTheDocument())
    expect(screen.queryByText(/нет опубликованного расписания/)).not.toBeInTheDocument()
  })

  it('показывает пустое состояние без рейсов', () => {
    renderPanel([])
    expect(screen.getByText('В каталоге нет рейсов с расписанием.')).toBeInTheDocument()
    expect(requested).toHaveLength(0)
  })

  it('показывает ошибку запроса, не превращая её в сценарий', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) }) as unknown as Response))
    renderPanel()
    await waitFor(() => expect(screen.getByText(/HTTP 404/)).toBeInTheDocument())
    expect(screen.queryByText('Успевает')).not.toBeInTheDocument()
  })

  it('выбирает рейс того же паттерна, что и у ТС, а не самый свежий', async () => {
    const onRoute = { ...vehicle, routePatternId: 'route_pattern_test' }
    render(<WhatIfPanel
      vehicle={onRoute} vehicleName="ТС 664030" hasSchedule
      occurrences={[
        { ...occurrence('occ_fresh', 4, 'route_pattern_other'), startsAt: '2026-01-06T18:00:00Z' },
        occurrence('occ_same', 4, 'route_pattern_test'),
      ]}
    />)
    await waitFor(() => expect(requested).toHaveLength(1))
    expect(new URL(requested[0], 'http://localhost').searchParams.get('occurrenceId')).toBe('occ_same')
  })

  it('без pattern у ТС берёт самый свежий рейс', async () => {
    renderPanel([
      { ...occurrence('occ_old', 4, 'route_pattern_other'), startsAt: '2026-01-06T08:00:00Z' },
      { ...occurrence('occ_new', 4, 'route_pattern_other'), startsAt: '2026-01-06T18:00:00Z' },
    ])
    await waitFor(() => expect(requested).toHaveLength(1))
    expect(new URL(requested[0], 'http://localhost').searchParams.get('occurrenceId')).toBe('occ_new')
  })
})
