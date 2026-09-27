package main

import (
	"flag"
	"fmt"
	"log"

	"mt_predictor/catalog"
)

func main() {
	catalogPath := flag.String("catalog", "data/generated/route_catalog.json", "derived route catalog")
	trafficPath := flag.String("traffic", "", "decoded traffic.csv to replay")
	outputPath := flag.String("output", "data/generated/replay_report.json", "JSON replay report")
	flag.Parse()
	if *trafficPath == "" {
		log.Fatal("-traffic is required")
	}
	routeCatalog, err := catalog.Load(*catalogPath)
	if err != nil {
		log.Fatal(err)
	}
	report, err := catalog.ReplayCSV(routeCatalog, *trafficPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := catalog.WriteReplayReport(report, *outputPath); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("replayed=%d applied=%d ignored_older=%d statuses=%v\n", report.InputRows, report.AppliedRows, report.IgnoredOlderRows, report.StatusCounts)
}
