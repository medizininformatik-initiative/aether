package pipeline

import "github.com/medizininformatik-initiative/aether/internal/lib"

// groupSizeStats collects the sizes of the resources and batches that one
// attribute group sends to the flattener.
type groupSizeStats struct {
	groupName            string
	budgetBytes          int
	logger               *lib.Logger
	buckets              [5]int
	resourceCount        int
	totalBytes           int
	batchCount           int
	largestBatchBytes    int
	largestResource      string
	largestResourceBytes int
}

func (s *groupSizeStats) addResource(ref string, size int) {
	s.resourceCount++
	s.totalBytes += size
	s.buckets[sizeBucket(size)]++
	if size > s.largestResourceBytes {
		s.largestResource = ref
		s.largestResourceBytes = size
	}
	if size > s.budgetBytes {
		s.logger.Warn("Resource is larger than the batch budget of its group",
			"group_name", s.groupName,
			"resource", ref,
			"resource_bytes", size,
			"budget_bytes", s.budgetBytes)
	}
}

// sizeBucketLimits are the exclusive upper bounds, in bytes, of all buckets but the last.
var sizeBucketLimits = [4]int{1_000, 10_000, 100_000, 1_000_000}

func sizeBucket(size int) int {
	for i, limit := range sizeBucketLimits {
		if size < limit {
			return i
		}
	}
	return len(sizeBucketLimits)
}

func (s *groupSizeStats) addBatch(size int) {
	s.batchCount++
	s.largestBatchBytes = max(s.largestBatchBytes, size)
}

func (s *groupSizeStats) logSummary() {
	s.logger.Info("Flattening group summary",
		"group_name", s.groupName,
		"resource_count", s.resourceCount,
		"total_bytes", s.totalBytes,
		"batch_count", s.batchCount,
		"largest_batch_bytes", s.largestBatchBytes,
		"largest_resource", s.largestResource,
		"largest_resource_bytes", s.largestResourceBytes)
	s.logger.Debug("Flattening group resource sizes",
		"group_name", s.groupName,
		"lt_1kb", s.buckets[0],
		"lt_10kb", s.buckets[1],
		"lt_100kb", s.buckets[2],
		"lt_1mb", s.buckets[3],
		"ge_1mb", s.buckets[4])
}
