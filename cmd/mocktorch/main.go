// Command mocktorch starts a mock TORCH server for progress demos. Point an
// aether config at it and watch the extraction progress in the terminal.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/medizininformatik-initiative/aether/internal/mocktorch"
)

func main() {
	addr := flag.String("addr", ":8086", "listen address")
	cohortSize := flag.Int("cohort-size", 1200, "number of patients in the simulated cohort")
	batchSize := flag.Int("batch-size", 100, "patients per batch")
	pollsPerBatch := flag.Int("polls-per-batch", 5, "status polls until a batch completes")
	flag.Parse()

	server := mocktorch.New(mocktorch.Config{
		CohortSize:    *cohortSize,
		BatchSize:     *batchSize,
		PollsPerBatch: *pollsPerBatch,
	})

	fmt.Printf("mock TORCH listening on %s (cohort %d, batch size %d, %d polls per batch)\n",
		*addr, *cohortSize, *batchSize, *pollsPerBatch)
	log.Fatal(http.ListenAndServe(*addr, server.Handler()))
}
