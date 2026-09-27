import type { RiskLevel, TelemetryState } from '../types'
import { riskLabel, telemetryLabel } from '../utils'

export function RiskBadge({ risk }: { risk: RiskLevel }) {
  return <span className={`status status--${risk}`}><span aria-hidden="true" className="status__dot" />{riskLabel[risk]}</span>
}

export function TelemetryBadge({ state }: { state: TelemetryState }) {
  return <span className={`telemetry telemetry--${state}`}>{telemetryLabel[state]}</span>
}
