import { useEffect, useState } from 'react'
import type { Incident } from '../api/types'
import { routeDisplayName, routeDirection, stopDisplayName } from '../domain/vehiclePresentation'
import { useLiveMap } from '../live/liveMapContext'
import { formatIncidentDate, formatIncidentDelay, formatIncidentEvidence, incidentReason } from '../domain/incidentPresentation'

const labels = { active: 'Текущий', awaiting_result: 'Ожидает результата', resolved: 'Результат известен', cancelled: 'Снят' } as const

export function IncidentCard({ incidentId, fallback, onClose }: { incidentId: string, fallback?: Incident, onClose: () => void }) {
  const { catalog } = useLiveMap()
  const [loadedIncident, setLoadedIncident] = useState<Incident>()
  const [notice, setNotice] = useState('')
  const incident = fallback ?? loadedIncident
  useEffect(() => {
    if (fallback) return
    let mounted = true
    fetch(`/api/v1/incidents/${incidentId}`).then((response) => {
      if (!response.ok) throw new Error()
      return response.json() as Promise<Incident>
    }).then((value) => { if (mounted) setLoadedIncident(value) })
      .catch(() => { if (mounted) setNotice('Не удалось загрузить инцидент') })
    return () => { mounted = false }
  }, [fallback, incidentId])
  const route = catalog?.routes.find((item) => item.id === incident?.routePatternId)
  const evidence = formatIncidentEvidence(incident?.evidence, Boolean(incident?.scenarioId))
  const mock = () => setNotice('Функция будет подключена позже')

  return <div className="incident-card-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <aside className="incident-card">
      <header><div><small>Карточка инцидента</small><h2>{incident ? `ТС ${incident.unitId}` : 'Загрузка…'}</h2></div><button onClick={onClose}>×</button></header>
      {incident ? <>
        <div className="incident-card__status">{labels[incident.status]}</div>
        <dl>
          <dt>Маршрут</dt><dd>{routeDisplayName(route)}<small>{routeDirection(route)}</small></dd>
          <dt>Остановка</dt><dd>{stopDisplayName(incident.targetStop)}</dd>
          <dt>Прогноз</dt><dd>{formatIncidentDelay(incident.predictedDelaySeconds)}</dd>
          <dt>Плановое прибытие</dt><dd>{formatIncidentDate(incident.targetPlannedAt)}</dd>
          <dt>Создан</dt><dd>{formatIncidentDate(incident.createdAt)}</dd>
          {incident.status === 'resolved' ? <>
            <dt>Результат</dt><dd>{incident.outcome === 'occurred' ? 'Инцидент сбылся' : 'Инцидент не сбылся'}</dd>
            <dt>Фактическое отклонение</dt><dd>{formatIncidentDelay(incident.actualDelaySeconds)}</dd>
            <dt>Фактическое прибытие</dt><dd>{formatIncidentDate(incident.actualArrivalAt)}</dd>
          </> : null}
        </dl>
        <section className="incident-card__reason">
          <h3>Возможная причина</h3>
          <p>{incidentReason(incident.reason)}</p>
          {incident.scenarioId ? <span className="incident-card__demo">Тестовый сценарий</span> : null}
          {evidence.length ? <dl>{evidence.map((item) => <div key={item.key}><dt>{item.label}</dt><dd>{item.value}</dd></div>)}</dl> : null}
        </section>
        {incident.status === 'resolved' ? <section><h3>Действия оператора</h3><p>Действия не зарегистрированы. Хранение будет добавлено позже.</p></section> : <section><h3>Реакция диспетчера</h3><div className="incident-card__actions"><button onClick={mock}>Связаться с водителем</button><button onClick={mock}>Скорректировать движение</button><button onClick={mock}>Запросить резерв</button></div></section>}
      </> : null}
      {notice ? <div className="incident-card__notice">{notice}</div> : null}
    </aside>
  </div>
}
