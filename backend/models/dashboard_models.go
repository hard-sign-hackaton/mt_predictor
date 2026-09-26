package models

// Карта с остановками
type Map []BusStop
type BusStop struct {
	ID      int64
	Lon     float64
	Lat     float64
	Address string
}

// Маршруты
type Routes []Routes
type Route struct {
	ID    int64
	Stops []int64 // Список айди остановок по порядку
}

// Телеметрия автобуса
type BusTelemetry struct {
	ID      int64
	Lon     float64
	Lat     float64
	Speed   float64
	Heading float64
}

// Инциденты
type Incident struct {
	ID             int64
	PredictedDelay float64
	Level          byte
	Descrption     string
}
