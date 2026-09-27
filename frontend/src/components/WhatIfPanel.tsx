import { useEffect, useMemo, useState } from 'react'
import { fetchWhatIf, type WhatIfQuery } from '../api/whatif'
import type { RouteOccurrence, VehicleState, WhatIfMode, WhatIfReport } from '../api/types'
import {
  whatIfDistance,
  whatIfDuration,
  whatIfModeLabel,
  whatIfReduction,
  whatIfTime,
  whatIfUnavailableText,
  whatIfVerdictLabel,
  whatIfVerdictTone,
} from '../domain/whatifPresentation'

/** Скорость перегона без пассажиров: допущение сценария, а не измерение. */
const EMPTY_SPEED_KMH = 30
/** Сколько остановок с наибольшим сокращением интервала показывать. */
const TOP_STOPS = 8

interface WhatIfPanelProps {
  vehicle: VehicleState
  vehicleName: string
  occurrences: RouteOccurrence[]
  hasSchedule: boolean
}

/** Граница разбиения по умолчанию — середина рейса, самый полезный сценарий. */
function middleOf(occurrence: RouteOccurrence | undefined): number {
  if (!occurrence || occurrence.calls.length === 0) return 0
  return Math.floor(occurrence.calls.length / 2)
}

function sameQuery(first: WhatIfQuery, second: WhatIfQuery): boolean {
  return first.unitId === second.unitId
    && first.occurrenceId === second.occurrenceId
    && first.joinCallIndex === second.joinCallIndex
    && first.mode === second.mode
}

function deadheadReason(verdict?: string): string {
  if (verdict === 'no_position') return 'у ТС нет пригодных координат'
  if (verdict === 'unknown_vehicle') return 'ТС ещё не появилось в потоке телеметрии'
  if (verdict === 'empty_occurrence') return 'у рейса нет событий расписания'
  if (verdict === 'run_in_past') return 'граница разбиения прошла, рейс уже состоялся'
  if (verdict === 'run_too_far') return 'граница разбиения дальше суток, запас пока не имеет смысла'
  return 'данных недостаточно'
}

export function WhatIfPanel({ vehicle, vehicleName, occurrences, hasSchedule }: WhatIfPanelProps) {
  const sorted = useMemo(
    () => [...occurrences].sort((first, second) => second.startsAt.localeCompare(first.startsAt)),
    [occurrences],
  )
  // Выбор пользователя. joinCallIndex остаётся неопределённым, пока граница не
  // выбрана вручную, и тогда выводится середина рейса. Так состояние не нужно
  // сбрасывать в эффекте.
  const [occurrenceChoice, setOccurrenceChoice] = useState<string>()
  const [chosenIndex, setChosenIndex] = useState<number>()
  const [mode, setMode] = useState<WhatIfMode>('relieve')
  const [result, setResult] = useState<{ query: WhatIfQuery; report: WhatIfReport }>()
  const [failure, setFailure] = useState<{ query: WhatIfQuery; message: string }>()

  // По умолчанию берём рейс того же паттерна, что и у ТС, иначе — самый свежий.
  const preferred = useMemo(
    () => sorted.find((occurrence) => occurrence.routePatternId === vehicle.routePatternId) ?? sorted[0],
    [sorted, vehicle.routePatternId],
  )
  const selected = useMemo(
    () => sorted.find((occurrence) => occurrence.id === occurrenceChoice) ?? preferred,
    [occurrenceChoice, preferred, sorted],
  )
  const maxIndex = Math.max(0, (selected?.calls.length ?? 1) - 1)
  const joinCallIndex = Math.min(chosenIndex ?? middleOf(selected), maxIndex)

  const query = useMemo<WhatIfQuery | undefined>(() => selected && ({
    unitId: vehicle.unitId, occurrenceId: selected.id, joinCallIndex,
    mode, emptySpeedKmh: EMPTY_SPEED_KMH, topStops: TOP_STOPS,
  }), [joinCallIndex, mode, selected, vehicle.unitId])

  useEffect(() => {
    if (!query) return
    const controller = new AbortController()
    fetchWhatIf(query, controller.signal)
      .then((report) => {
        if (!controller.signal.aborted) setResult({ query, report })
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return
        setFailure({ query, message: cause instanceof Error ? cause.message : 'Сценарий What-if недоступен' })
      })
    return () => controller.abort()
  }, [query])

  // Ответ показывается только для того запроса, который его породил, иначе на
  // экране на секунду остались бы числа от предыдущей границы разбиения.
  const report = result && query && sameQuery(result.query, query) ? result.report : undefined
  const error = failure && query && sameQuery(failure.query, query) ? failure.message : undefined

  if (!sorted.length) {
    return <section className="whatif">
      <h3>Выпуск дополнительного ТС</h3>
      <p className="data-note data-note--missing">В каталоге нет рейсов с расписанием.</p>
    </section>
  }

  const verdict = report?.deadhead.verdict

  return <section className="whatif">
    <div className="section-heading">
      <h3>Выпуск дополнительного ТС</h3>
      <small className="data-note">read-only</small>
    </div>

    {!hasSchedule && <p className="data-note">
      У этого ТС нет опубликованного расписания, поэтому оно не занято в рейсе и подходит на роль кандидата.
    </p>}

    <label>Сценарий
      <select aria-label="Режим сценария" value={mode} onChange={(event) => setMode(event.target.value as WhatIfMode)}>
        <option value="relieve">Разделить рейс между двумя ТС</option>
        <option value="duplicate">Дублировать вторую половину рейса</option>
      </select>
    </label>

    <label>Рейс
      <select aria-label="Рейс для сценария" value={selected?.id} onChange={(event) => {
        setOccurrenceChoice(event.target.value)
        setChosenIndex(undefined)
      }}>
        {sorted.map((occurrence) => <option key={occurrence.id} value={occurrence.id}>
          {occurrence.routePatternId.replace('route_pattern_', 'pattern ')} · {whatIfTime(occurrence.startsAt)} · {occurrence.calls.length} call
        </option>)}
      </select>
    </label>

    <label>Кандидат ведёт рейс с call #{joinCallIndex} из {maxIndex}
      <input aria-label="Граница разбиения рейса" type="range" min={0} max={maxIndex} value={joinCallIndex}
        onChange={(event) => setChosenIndex(Number(event.target.value))} />
    </label>

    {error && <p className="data-note data-note--missing">{error}</p>}
    {!report && !error && <p className="data-note">Расчёт сценария…</p>}

    {report && <>
      <p className={`whatif__verdict ${whatIfVerdictTone(verdict)}`}>
        {whatIfVerdictLabel(verdict)}
        <small>{`${vehicleName} · ${whatIfModeLabel(report.mode)}`}</small>
      </p>

      <dl className="whatif__grid">
        <dt>Перегон до границы</dt><dd>{whatIfDistance(report.deadhead.meters)}</dd>
        <dt>Время перегона</dt><dd>{whatIfDuration(report.deadhead.seconds)}</dd>
        <dt>Запас по времени</dt><dd>{whatIfDuration(report.deadhead.slackSeconds)}</dd>
        <dt>Скорость перегона</dt><dd>{report.deadhead.assumedEmptySpeedKmh} км/ч (допущение)</dd>
        <dt>Граница разбиения</dt><dd>{whatIfTime(report.target.joinPlannedAt)}</dd>
        <dt>Остановка на границе</dt><dd>{report.target.joinStopAddress || report.target.joinStopId}</dd>
      </dl>

      {report.deadhead.unavailableReason && <p className="data-note data-note--missing">
        {`Перегон не рассчитан: ${deadheadReason(report.deadhead.unavailableReason)}`}
      </p>}

      {report.conflict.detected && <p className="data-note data-note--missing">
        {`Конфликт: у этого ТС уже есть пересекающийся рейс ${report.conflict.occurrenceId} (${whatIfTime(report.conflict.validFrom)} — ${whatIfTime(report.conflict.validTo)}).`}
      </p>}

      <h4>Как делится рейс</h4>
      <dl className="whatif__grid">
        <dt>Событий у действующего ТС</dt><dd>{report.partition.callsVehicleOne}</dd>
        <dt>Событий у кандидата</dt><dd>{report.partition.callsVehicleTwo}</dd>
        <dt>Рейс целиком</dt><dd>{whatIfDuration(report.partition.fullRunSeconds)}</dd>
        <dt>Самый длинный рейс</dt><dd>{whatIfDuration(report.partition.longestVehicleRunSeconds)}</dd>
        <dt>Остановок с одним ТС</dt><dd>{report.partition.stopsServedByOne}</dd>
        <dt>Остановок с двумя ТС</dt><dd>{report.partition.stopsServedByTwo}</dd>
      </dl>

      <h4>Интервал обслуживания</h4>
      <dl className="whatif__grid">
        <dt>Средний интервал до</dt><dd>{whatIfDuration(report.serviceInterval.meanHeadwayBeforeSeconds)}</dd>
        <dt>Средний интервал после</dt><dd>{whatIfDuration(report.serviceInterval.meanHeadwayAfterSeconds)}</dd>
      </dl>

      {report.serviceInterval.improvedStops.length === 0
        ? <p className="data-note">
            Интервал обслуживания не меняется: опубликованное расписание остаётся прежним, меняется только объём работы на одно ТС.
          </p>
        : <table className="whatif__stops">
            <thead><tr><th>Остановка</th><th>До</th><th>После</th><th>Интервал</th></tr></thead>
            <tbody>
              {report.serviceInterval.improvedStops.map((stop) => <tr key={stop.stopId}>
                <td>{stop.address || stop.stopId}</td>
                <td>{whatIfDuration(stop.headwayBeforeSeconds)}</td>
                <td>{whatIfDuration(stop.headwayAfterSeconds)}</td>
                <td>{whatIfReduction(stop.reduction)}</td>
              </tr>)}
            </tbody>
          </table>}

      <p className="data-note data-note--missing">{whatIfUnavailableText(report)}</p>
      <details className="whatif__assumptions">
        <summary>Допущения</summary>
        <ul>{report.assumptions.map((assumption) => <li key={assumption}>{assumption}</li>)}</ul>
      </details>
    </>}
  </section>
}
