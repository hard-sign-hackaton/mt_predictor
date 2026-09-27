import type { WhatIfMode, WhatIfReport } from './types'

/** Параметры сценария «выпуск дополнительного ТС». */
export interface WhatIfQuery {
  /** ТС-кандидат, выбранный на карте. */
  unitId: number
  /** Рейс, на который планируется выпуск. */
  occurrenceId: string
  /** Событие расписания, с которого кандидат ведёт рейс. */
  joinCallIndex: number
  mode: WhatIfMode
  /** Скорость перегона без пассажиров; допущение, а не факт. */
  emptySpeedKmh: number
  /** Сколько остановок с наибольшим сокращением интервала показать. */
  topStops: number
}

/**
 * Запрашивает read-only сценарий What-if.
 *
 * Запрос не меняет состояние backend: инциденты не создаются, прогнозы не
 * записываются, поэтому повторный вызов с теми же аргументами безопасен.
 */
export async function fetchWhatIf(query: WhatIfQuery, signal?: AbortSignal): Promise<WhatIfReport> {
  const parameters = new URLSearchParams({
    unitId: String(query.unitId),
    occurrenceId: query.occurrenceId,
    joinCallIndex: String(query.joinCallIndex),
    mode: query.mode,
    emptySpeedKmh: String(query.emptySpeedKmh),
    topStops: String(query.topStops),
  })
  const path = `/api/v1/whatif?${parameters.toString()}`
  const response = await fetch(path, { signal })
  if (!response.ok) throw new Error(`Сценарий What-if недоступен: HTTP ${response.status}`)
  return response.json() as Promise<WhatIfReport>
}
