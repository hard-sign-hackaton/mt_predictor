import type { WhatIfMode, WhatIfReport, WhatIfVerdict } from '../api/types'

/** Человекочитаемый вывод о выполнимости сценария. */
const verdictLabel: Record<WhatIfVerdict, string> = {
  feasible: 'Успевает',
  tight: 'Успевает с опозданием',
  infeasible: 'Не успевает',
  run_in_past: 'Рейс уже прошёл',
  run_too_far: 'Рейс слишком далеко',
  unknown_vehicle: 'ТС ещё не в эфире',
  no_position: 'Нет координат ТС',
  stale_position: 'Позиция устарела',
  empty_occurrence: 'У рейса нет расписания',
}

/** Класс оформления вердикта; вынесен, чтобы UI не выбирал стили по смыслу. */
const verdictTone: Record<WhatIfVerdict, string> = {
  feasible: 'whatif__verdict--ok',
  tight: 'whatif__verdict--warn',
  infeasible: 'whatif__verdict--bad',
  run_in_past: 'whatif__verdict--missing',
  run_too_far: 'whatif__verdict--missing',
  unknown_vehicle: 'whatif__verdict--missing',
  no_position: 'whatif__verdict--missing',
  stale_position: 'whatif__verdict--warn',
  empty_occurrence: 'whatif__verdict--missing',
}

/**
 * Оформление режима. Формулировки намеренно различают «сократился объём
 * работы» и «сократился интервал»: во втором случае работа дублируется.
 */
const modeLabel: Record<WhatIfMode, string> = {
  relieve: 'Разделение рейса',
  duplicate: 'Дублирование рейса',
}

export function whatIfVerdictLabel(verdict?: WhatIfVerdict): string {
  return verdict ? verdictLabel[verdict] : 'Не рассчитано'
}

export function whatIfVerdictTone(verdict?: WhatIfVerdict): string {
  return verdict ? verdictTone[verdict] : 'whatif__verdict--missing'
}

export function whatIfModeLabel(mode: WhatIfMode): string {
  return modeLabel[mode]
}

/**
 * Причина, по которой сокращение задержки по графику не вычисляется.
 * Формулировка объясняет ограничение данных, а не отказ системы.
 */
export function whatIfUnavailableText(report: WhatIfReport): string {
  if (report.unavailableReason === 'no_capacity_model') {
    return 'Сокращение задержки по графику не вычисляется: в данных ровно одно ТС на маршрут, модель пропускной способности отсутствует.'
  }
  return 'Часть сценария не рассчитана из-за неполных данных.'
}

/** Форматирует длительность в компактном виде: 95 с, 12 мин 30 с, 1 ч 05 мин. */
export function whatIfDuration(seconds?: number): string {
  if (seconds === undefined) return 'нет данных'
  const rounded = Math.round(Math.abs(seconds))
  const sign = seconds < 0 ? '−' : ''
  if (rounded < 60) return `${sign}${rounded} с`
  const minutes = Math.floor(rounded / 60)
  if (minutes < 60) {
    const rest = rounded % 60
    return rest ? `${sign}${minutes} мин ${rest} с` : `${sign}${minutes} мин`
  }
  return `${sign}${Math.floor(minutes / 60)} ч ${String(minutes % 60).padStart(2, '0')} мин`
}

/** Форматирует расстояние в метрах или километрах. */
export function whatIfDistance(meters?: number): string {
  if (meters === undefined) return 'нет данных'
  if (meters < 1000) return `${Math.round(meters)} м`
  return `${(meters / 1000).toFixed(1)} км`
}

/** Процент сокращения интервала; отсутствие значения означает «не менялось». */
export function whatIfReduction(reduction?: number): string {
  if (reduction === undefined) return '—'
  if (reduction <= 0.0001) return 'не изменился'
  return `−${Math.round(reduction * 100)}%`
}

export function whatIfTime(value?: string): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('ru-RU', {
    day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit',
  }).format(new Date(value))
}
