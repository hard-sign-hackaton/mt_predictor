import { useMemo, useState } from 'react'
import { riskFromDelaySeconds, type RiskLevel } from '../api/risk'
import type { DelayPrediction, Incident } from '../api/types'
import { useLiveMap } from '../live/liveMapContext'

type RiskFilter = 'all' | RiskLevel

const riskLabel: Record<RiskLevel, string> = {
  normal: 'Низкая',
  watch: 'Средняя · WATCH',
  high: 'Высокая · HIGH',
}

function formatDelay(value: number) {
  const sign = value >= 0 ? '+' : '−'
  const absolute = Math.round(Math.abs(value))
  const minutes = Math.floor(absolute / 60)
  const seconds = absolute % 60
  return `${sign}${minutes}:${String(seconds).padStart(2, '0')}`
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat('ru-RU', {
    day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).format(new Date(value))
}

function horizonMinutes(prediction?: DelayPrediction) {
  if (!prediction) return '—'
  return `${Math.round((new Date(prediction.targetPlannedAt).getTime() - new Date(prediction.predictionTime).getTime()) / 60_000)} мин`
}

function searchable(incident: Incident) {
  return `${incident.id} ${incident.unitId} ${incident.trId} ${incident.routePatternId} ${incident.targetStop.id} ${incident.targetStop.address} ${incident.reasonCode ?? ''} ${incident.reason ?? ''}`.toLowerCase()
}

const evidenceLabels: Record<string, string> = {
  door_open_duration_s: 'Двери открыты, с',
  stationary_duration_s: 'Стоянка, с',
  speed_mean_5m_kmh: 'Средняя скорость за 5 мин, км/ч',
  congestion_index: 'Индекс загруженности',
  last_gps_age_s: 'Возраст GPS-точки',
  valid_gps_points_5m: 'Валидные GPS-точки за 5 мин',
  route_deviation_m: 'Отклонение от маршрута',
}

function formatEvidence(evidence?: Record<string, number>) {
  if (!evidence) return ''
  return Object.entries(evidence)
    .map(([key, value]) => `${evidenceLabels[key] ?? key}: ${value}`)
    .join(' · ')
}

export function IncidentsPage() {
  const { catalog, incidents, predictions, connection } = useLiveMap()
  const [query, setQuery] = useState('')
  const [riskFilter, setRiskFilter] = useState<RiskFilter>('all')

  const predictionsByID = useMemo(() => new Map(predictions.map((prediction) => [prediction.id, prediction])), [predictions])
  const rows = useMemo(() => {
    if (!catalog) return []
    return incidents
      .filter((incident) => searchable(incident).includes(query.trim().toLowerCase()))
      .filter((incident) => riskFilter === 'all' || riskFromDelaySeconds(incident.predictedDelaySeconds, catalog.riskThresholds) === riskFilter)
      .sort((left, right) => right.predictedDelaySeconds - left.predictedDelaySeconds)
  }, [catalog, incidents, query, riskFilter])

  if (!catalog) return <div className="incidents-loading">Загрузка данных инцидентов…</div>

  return <main className="incidents-page">
    <header className="incidents-heading">
      <div><h1>Инциденты прогнозируемых задержек</h1><p>Только активные прогнозы первой остановки в горизонте 10–15 минут. Причина показывается, только если её удалось обосновать входными сигналами.</p></div>
      <div><strong>{incidents.length}</strong><span>активных</span><small>{connection === 'live' ? 'LIVE' : connection}</small></div>
    </header>

    <section className="incidents-filters" aria-label="Фильтры инцидентов">
      <input aria-label="Поиск инцидентов" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="ТС, tr_id, остановка или ID паттерна" />
      <label>Критичность<select value={riskFilter} onChange={(event) => setRiskFilter(event.target.value as RiskFilter)}>
        <option value="all">Все активные</option>
        <option value="normal">Низкая</option>
        <option value="watch">Средняя · WATCH</option>
        <option value="high">Высокая · HIGH</option>
      </select></label>
      {(query || riskFilter !== 'all') && <button onClick={() => { setQuery(''); setRiskFilter('all') }}>Сбросить</button>}
    </section>

    <section className="incidents-table-wrap">
      <table className="incidents-table">
        <thead><tr><th>Критичность</th><th>Транспорт</th><th>Маршрутный контекст</th><th>Целевая остановка</th><th>Причина</th><th>Прогноз</th><th>Горизонт / план</th><th>Обновлено</th></tr></thead>
        <tbody>{rows.map((incident) => {
          const prediction = predictionsByID.get(incident.predictionId)
          const risk = riskFromDelaySeconds(incident.predictedDelaySeconds, catalog.riskThresholds)
          return <tr key={incident.id}>
            <td><span className={`incident-risk incident-risk--${risk}`}>{riskLabel[risk]}</span></td>
            <td><strong>ТС {incident.unitId}</strong><small>tr_id {incident.trId}</small></td>
            <td><strong>{incident.routePatternId.replace('route_pattern_', 'pattern ')}</strong><small>вычисленный паттерн, не официальный номер</small></td>
            <td><strong>{incident.targetStop.address || 'Адрес не указан'}</strong><small>stop {incident.targetStop.id} · action {incident.targetActionItemId}</small></td>
            <td><strong>{incident.reason || 'Не определена по доступным сигналам'}</strong><small>{incident.scenarioId ? `Тестовый сценарий: ${incident.scenarioId}` : incident.reasonCode || 'Недостаточно данных'}</small>{formatEvidence(incident.evidence) && <small>{formatEvidence(incident.evidence)}</small>}</td>
            <td><strong className="incident-delay">{formatDelay(incident.predictedDelaySeconds)}</strong><small>прогноз задержки</small></td>
            <td><strong>{horizonMinutes(prediction)}</strong><small>{prediction ? `план ${formatDateTime(prediction.targetPlannedAt)}` : 'план недоступен'}</small></td>
            <td><strong>{formatDateTime(incident.updatedAt)}</strong><small>создан {formatDateTime(incident.createdAt)}</small></td>
          </tr>
        })}</tbody>
      </table>
      {!rows.length && <div className="incidents-empty"><strong>{incidents.length ? 'Нет инцидентов по выбранным фильтрам' : 'Активных инцидентов пока нет'}</strong><span>{incidents.length ? 'Измените поиск или критичность.' : 'Инцидент появится после подтверждения текущей задержки и ML-прогноза больше 120 секунд.'}</span></div>}
    </section>
  </main>
}
