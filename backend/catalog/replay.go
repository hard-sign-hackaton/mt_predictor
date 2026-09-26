package catalog

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"
)

type ReplayReport struct {
	InputRows             int                 `json:"input_rows"`
	AppliedRows           int                 `json:"applied_rows"`
	IgnoredOlderRows      int                 `json:"ignored_older_rows"`
	RealBindings          int                 `json:"real_bindings"`
	RealWithSchedule      int                 `json:"real_with_schedule"`
	RealWithoutSchedule   int                 `json:"real_without_schedule"`
	StatusCounts          map[MatchStatus]int `json:"status_counts"`
	GeometryQualityCounts map[string]int      `json:"geometry_quality_counts"`
	Examples              []MatchResult       `json:"examples"`
}

func ReplayCSV(catalog *Catalog, path string) (ReplayReport, error) {
	file, err := os.Open(path)
	if err != nil {
		return ReplayReport{}, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return ReplayReport{}, err
	}
	columns := map[string]int{}
	for index, name := range header {
		columns[name] = index
	}
	required := []string{"unit_id", "event_time", "receive_time", "location_valid", "lon", "lat", "speed", "heading", "is_hist_data"}
	for _, name := range required {
		if _, exists := columns[name]; !exists {
			return ReplayReport{}, fmt.Errorf("traffic CSV misses column %s", name)
		}
	}
	var records []Telemetry
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ReplayReport{}, readErr
		}
		telemetry, parseErr := parseTelemetry(row, columns)
		if parseErr != nil {
			return ReplayReport{}, parseErr
		}
		records = append(records, telemetry)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].ReceiveTime.Equal(records[j].ReceiveTime) {
			return records[i].EventTime.Before(records[j].EventTime)
		}
		return records[i].ReceiveTime.Before(records[j].ReceiveTime)
	})

	report := ReplayReport{
		InputRows: len(records), StatusCounts: map[MatchStatus]int{}, GeometryQualityCounts: map[string]int{},
	}
	for _, binding := range catalog.VehicleBindings {
		if binding.Synthetic {
			continue
		}
		report.RealBindings++
		if binding.HasSchedule {
			report.RealWithSchedule++
		} else {
			report.RealWithoutSchedule++
		}
	}
	state := NewLiveState(NewMatcher(catalog))
	seenExample := map[string]bool{}
	for _, telemetry := range records {
		match, applied := state.Apply(telemetry)
		if !applied {
			report.IgnoredOlderRows++
			continue
		}
		report.AppliedRows++
		report.StatusCounts[match.Status]++
		if match.GeometryQuality != "" {
			report.GeometryQualityCounts[match.GeometryQuality]++
		}
		key := fmt.Sprintf("%s:%d", match.Status, match.TRID)
		if len(report.Examples) < 25 && !seenExample[key] {
			report.Examples = append(report.Examples, match)
			seenExample[key] = true
		}
	}
	return report, nil
}

func WriteReplayReport(report ReplayReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func parseTelemetry(row []string, columns map[string]int) (Telemetry, error) {
	unit, err := strconv.ParseUint(row[columns["unit_id"]], 10, 32)
	if err != nil {
		return Telemetry{}, err
	}
	eventTime, err := parseCSVTime(row[columns["event_time"]])
	if err != nil {
		return Telemetry{}, err
	}
	receiveTime, err := parseCSVTime(row[columns["receive_time"]])
	if err != nil {
		return Telemetry{}, err
	}
	valid, err := strconv.ParseBool(row[columns["location_valid"]])
	if err != nil {
		return Telemetry{}, err
	}
	historical, err := strconv.ParseBool(row[columns["is_hist_data"]])
	if err != nil {
		return Telemetry{}, err
	}
	parseFloat := func(name string) float64 {
		value, _ := strconv.ParseFloat(row[columns[name]], 64)
		return value
	}
	return Telemetry{
		UnitID: uint32(unit), Lon: parseFloat("lon"), Lat: parseFloat("lat"), Speed: parseFloat("speed"),
		Heading: parseFloat("heading"), EventTime: eventTime, ReceiveTime: receiveTime, Valid: valid,
		Historical: historical,
	}, nil
}

func parseCSVTime(value string) (time.Time, error) {
	formats := []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"}
	for _, format := range formats {
		if parsed, err := time.ParseInLocation(format, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
}
