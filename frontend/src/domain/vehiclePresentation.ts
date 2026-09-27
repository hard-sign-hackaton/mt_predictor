import type { MatchStatus, RoutePattern, Stop, StopReference, VehicleState } from '../api/types'

export type RouteVehicleFilter = 'all' | 'on_route' | 'not_on_route'
export type DelayVehicleFilter = 'all' | 'delayed' | 'on_time'

const matchStatusLabels: Record<MatchStatus, string> = {
  matched: 'Идёт по маршруту',
  matched_spatial: 'Идёт по маршруту',
  off_route: 'Вне маршрута',
  unmapped_unit: 'ТС не привязано',
  invalid_location: 'Нет геопозиции',
  no_schedule: 'Нет расписания',
  no_active_pattern: 'Нет активного рейса',
}

export function vehicleName(vehicle: VehicleState) {
  return `ТС ${vehicle.unitId}`
}

export function routeDisplayName(route?: RoutePattern) {
  if (!route?.officialRouteId) return 'Маршрут не определён'
  return `Маршрут №${route.officialRouteId}`
}

export function routeDirection(route?: RoutePattern) {
  return route?.officialRouteName || 'Направление не определено'
}

export function stopDisplayName(stop?: Stop | StopReference) {
  return stop?.address?.trim() || 'Название остановки неизвестно'
}

export function vehicleRouteName(vehicle: VehicleState, routesByID: Map<string, RoutePattern>) {
  return routeDisplayName(vehicle.routePatternId ? routesByID.get(vehicle.routePatternId) : undefined)
}

export function matchStatusLabel(status: MatchStatus) {
  return matchStatusLabels[status]
}

export function isOnRoute(vehicle: VehicleState) {
  return vehicle.matchStatus === 'matched' || vehicle.matchStatus === 'matched_spatial'
}

export function filterVehicles(vehicles: VehicleState[], routeFilter: RouteVehicleFilter, delayFilter: DelayVehicleFilter) {
  return vehicles.filter((vehicle) => {
    if (routeFilter === 'on_route' && !isOnRoute(vehicle)) return false
    if (routeFilter === 'not_on_route' && isOnRoute(vehicle)) return false
    if (delayFilter === 'delayed' && !(vehicle.currentDelaySeconds !== undefined && vehicle.currentDelaySeconds > 0)) return false
    if (delayFilter === 'on_time' && !(vehicle.currentDelaySeconds !== undefined && vehicle.currentDelaySeconds <= 0)) return false
    return true
  })
}
