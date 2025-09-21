// Package mockfs provides a mock filesystem implementation based on testing/fstest.MapFS,
// allowing for controlled error injection and latency simulation for testing purposes.
package mockfs

import (
	"testing/fstest"
)

// A MapFile describes a single file in a [MapFS].
// See [testing/fstest.MapFile].
type MapFile = fstest.MapFile
