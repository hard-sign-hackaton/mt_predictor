export function formatIncidentDelay(value?: number) {
  if (value === undefined) return '—'
  const sign = value >= 0 ? '+' : '−'
  const absolute = Math.round(Math.abs(value))
  return `${sign}${Math.floor(absolute / 60)}:${String(absolute % 60).padStart(2, '0')}`
}

export function formatIncidentDate(value?: string) {
  return value ? new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value)) : '—'
}

const evidencePresentation: Record<string, { label: string, format: (value: number) => string }> = {
  door_open_duration_s: { label: 'Двери открыты', format: seconds },
  stationary_duration_s: { label: 'Стоянка', format: seconds },
  speed_mean_5m_kmh: { label: 'Средняя скорость за 5 минут', format: (value) => `${formatNumber(value)} км/ч` },
  congestion_index: { label: 'Загруженность участка', format: (value) => `${Math.round(value * 100)}%` },
  last_gps_age_s: { label: 'Возраст последней GPS-точки', format: seconds },
  valid_gps_points_5m: { label: 'Валидных GPS-точек за 5 минут', format: (value) => formatNumber(value) },
  route_deviation_m: { label: 'Отклонение от маршрута', format: (value) => `${formatNumber(value)} м` },
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 }).format(value)
}

function seconds(value: number) {
  const rounded = Math.round(value)
  return rounded >= 60 ? `${Math.floor(rounded / 60)} мин ${rounded % 60} с` : `${rounded} с`
}

export function incidentReason(reason?: string) {
  return reason?.trim() || 'Причина не определена'
}

export function formatIncidentEvidence(evidence: Record<string, number> | undefined, includeUnknown = false) {
  return Object.entries(evidence ?? {}).flatMap(([key, value]) => {
    const presentation = evidencePresentation[key]
    if (presentation) return [{ key, label: presentation.label, value: presentation.format(value) }]
    return includeUnknown ? [{ key, label: `Дополнительный признак (${key})`, value: formatNumber(value) }] : []
  })
}
