import { useEffect, useState } from 'react'
import type { Incident, IncidentActions, OperatorAction } from '../api/types'
import { routeDisplayName, routeDirection, stopDisplayName } from '../domain/vehiclePresentation'
import { useLiveMap } from '../live/liveMapContext'
import { formatIncidentDate, formatIncidentDelay, formatIncidentEvidence, incidentReason } from '../domain/incidentPresentation'

const labels = { active: 'Текущий', awaiting_result: 'Ожидает результата', resolved: 'Результат известен', cancelled: 'Снят' } as const

export function IncidentCard({ incidentId, fallback, onClose }: { incidentId: string, fallback?: Incident, onClose: () => void }) {
  const { catalog } = useLiveMap()
  const [loadedIncident, setLoadedIncident] = useState<Incident>()
  const [notice, setNotice] = useState('')
  const [actions, setActions] = useState<IncidentActions>({ available: [], history: [] })
  const [sending, setSending] = useState('')
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
  useEffect(() => {
    let mounted = true
    fetch(`/api/v1/incidents/${incidentId}/actions`).then((response) => {
      if (!response.ok) throw new Error()
      return response.json() as Promise<IncidentActions>
    }).then((value) => { if (mounted) setActions(value) })
      .catch(() => { if (mounted) setNotice('Не удалось загрузить историю действий') })
    return () => { mounted = false }
  }, [incidentId])
  const route = catalog?.routes.find((item) => item.id === incident?.routePatternId)
  const evidence = formatIncidentEvidence(incident?.evidence, Boolean(incident?.scenarioId))
  const sendAction = async (actionCode: string) => {
    setSending(actionCode)
    setNotice('')
    try {
      const response = await fetch(`/api/v1/incidents/${incidentId}/actions`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ actionCode }) })
      if (!response.ok) throw new Error()
      const action = await response.json() as OperatorAction
      setActions((current) => ({ ...current, history: [action, ...current.history] }))
      setNotice('Сообщение отправлено')
    } catch {
      setNotice('Не удалось отправить сообщение')
    } finally {
      setSending('')
    }
  }

  return <div className="incident-card-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <aside className="incident-card">
      <header><div><small>Карточка инцидента</small><h2>{incident ? `ТС ${incident.unitId}` : 'Загрузка…'}</h2></div><button onClick={onClose}>×</button></header>
      {incident ? <>
        <div className="incident-card__status">{labels[incident.status]}</div>
        <dl>
          <dt>Маршрут</dt><dd>{routeDisplayName(route)}<small>{routeDirection(route)}</small></dd>
          <dt>Остановка</dt><dd>{stopDisplayName(incident.targetStop)}</dd>
          <dt>Прогноз</dt><dd>{formatIncidentDelay(incident.predictedDelaySeconds)}</dd>
          <dt>Текущая задержка</dt><dd>{formatIncidentDelay(incident.currentDelaySeconds)}</dd>
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
        {actions.available.length ? <section><h3>Реакция диспетчера</h3><div className="incident-card__actions">{actions.available.map((action) => <button key={action.code} disabled={Boolean(sending)} onClick={() => sendAction(action.code)}>{sending === action.code ? 'Отправка…' : action.label}<small>{action.recipient === 'driver' ? 'Водителю' : 'В диспетчерский штаб'}</small></button>)}</div></section> : null}
        <section className="incident-card__history"><h3>История действий</h3>{actions.history.length ? <ol>{actions.history.map((action) => <li key={action.id}><div><strong>{action.label}</strong><time>{formatIncidentDate(action.createdAt)}</time></div><p>{action.message}</p><small>{action.recipient === 'driver' ? 'Получатель: водитель' : 'Получатель: диспетчерский штаб'} · {action.status === 'pending' ? 'Ожидает обработки' : 'Обработано'}</small></li>)}</ol> : <p>Действия не зарегистрированы.</p>}</section>
      </> : null}
      {notice ? <div className="incident-card__notice">{notice}</div> : null}
    </aside>
  </div>
}
