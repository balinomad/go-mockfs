// Package mockfs provides a mock filesystem implementation based on testing/fstest.MapFS,
// allowing for controlled error injection and latency simulation for testing purposes.
package mockfs

import "testing/fstest"

// Stats records operation counts for verification in tests.
// Ensure field names match those accessed in fs.go's incrementStat.
type Stats struct {
	StatCalls    int
	OpenCalls    int
	ReadCalls    int
	WriteCalls   int
	ReadDirCalls int
	CloseCalls   int
}

// A MapFile describes a single file in a [MapFS].
// See [testing/fstest.MapFile].
type MapFile = fstest.MapFile
