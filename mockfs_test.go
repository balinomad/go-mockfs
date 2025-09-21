package mockfs_test

import (
	"errors"
	"io"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs"
)

// checkStats is a helper to check stats.
func checkStats(t *testing.T, m *mockfs.MockFS, expected [mockfs.NumOperations]int, context string) {
	t.Helper()
	actual := m.GetStats().Snapshot()
	if actual != expected {
		t.Errorf("%s: snapshot = %+v, want %+v", context, actual, expected)
	}
}

// testStatFile is a helper to verify stat operation.
func testStatFile(t *testing.T, mockFS *mockfs.MockFS, path, expectedName, expectedContent string) {
	t.Helper()
	info, err := mockFS.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	expectedSize := int64(len(expectedContent))
	if info.Name() != expectedName || info.Size() != expectedSize || info.IsDir() {
		t.Errorf("Stat returned unexpected info: %+v", info)
	}
}

// testOpenReadClose is a helper to verify open/read/close operations with stat tracking.
func testOpenReadClose(t *testing.T, mockFS *mockfs.MockFS, path, expectedContent string) {
	t.Helper()
	statsBefore := mockFS.GetStats()

	file, err := mockFS.Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Verify Open incremented the count
	statsAfterOpen := mockFS.GetStats()
	if statsAfterOpen.Count(mockfs.OpOpen) != statsBefore.Count(mockfs.OpOpen)+1 {
		t.Errorf("Expected mockfs.OpOpen to increment by 1")
	}

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("Expected %q, got %q", expectedContent, string(content))
	}

	// ReadAll can make multiple reads. Check that the count increased.
	statsAfterRead := mockFS.GetStats()
	if statsAfterRead.Count(mockfs.OpRead) <= statsBefore.Count(mockfs.OpRead) {
		t.Errorf("Expected mockfs.OpRead to increase from %d", statsBefore.Count(mockfs.OpRead))
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify Close incremented the count
	statsAfterClose := mockFS.GetStats()
	if statsAfterClose.Count(mockfs.OpClose) != statsBefore.Count(mockfs.OpClose)+1 {
		t.Errorf("Expected CloseCalls to increment by 1")
	}
}

// testReadFile is a helper to verify ReadFile operation with stat tracking.
func testReadFile(t *testing.T, mockFS *mockfs.MockFS, path, expectedContent string) {
	t.Helper()
	statsBefore := mockFS.GetStats()

	content, err := mockFS.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("Expected %q, got %q", expectedContent, string(content))
	}

	// Check cumulative stats after ReadFile
	statsAfter := mockFS.GetStats()
	if statsAfter.Count(mockfs.OpOpen) != statsBefore.Count(mockfs.OpOpen)+1 {
		t.Errorf("Expected mockfs.OpOpen to increment by 1")
	}
	if statsAfter.Count(mockfs.OpClose) != statsBefore.Count(mockfs.OpClose)+1 {
		t.Errorf("Expected CloseCalls to increment by 1")
	}
	if statsAfter.Count(mockfs.OpRead) <= statsBefore.Count(mockfs.OpRead) {
		t.Errorf("Expected mockfs.OpRead to increase")
	}
}

// TestDirectoryOperations verifies mockfs operations on directories.
// It tests Stat and ReadDir on a directory.
func TestDirectoryOperations(t *testing.T) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"dir/file.txt": {Data: []byte("in dir")},
		"dir":          {Mode: fs.ModeDir},
	})

	t.Run("StatDir", func(t *testing.T) {
		info, err := mockFS.Stat("dir")
		if err != nil {
			t.Fatalf("Stat dir failed: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("Stat dir returned non-dir info: %+v", info)
		}
		checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpStat: 1}, "After Stat dir")
	})

	t.Run("ReadDir", func(t *testing.T) {
		statsBefore := mockFS.GetStats()
		entries, err := mockFS.ReadDir("dir")
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "file.txt" {
			t.Errorf("ReadDir returned unexpected entries: %v", entries)
		}

		statsAfter := mockFS.GetStats()
		if statsAfter.Count(mockfs.OpReadDir) != statsBefore.Count(mockfs.OpReadDir)+1 {
			t.Errorf("Expected mockfs.OpReadDir to increment by 1")
		}
	})
}

// TestFileOperations exercises mockfs operations on files.
// It tests Stat, ReadFile, Open/Read/Close, and ReadFile on files.
func TestFileOperations(t *testing.T) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt":     {Data: []byte("test content")},
		"dir/file.txt": {Data: []byte("test content")},
		"dir":          {Mode: fs.ModeDir},
	})

	testFiles := []struct {
		name            string
		path            string
		expectedName    string
		expectedContent string
	}{
		{"test.txt", "test.txt", "test.txt", "test content"},
		{"dir/file.txt", "dir/file.txt", "file.txt", "test content"},
	}
	for _, tf := range testFiles {
		t.Run("Stat "+tf.name, func(t *testing.T) {
			testStatFile(t, mockFS, tf.path, tf.expectedName, tf.expectedContent)
		})

		t.Run("Open, Read, and Close "+tf.name, func(t *testing.T) {
			testOpenReadClose(t, mockFS, tf.path, tf.expectedContent)
		})

		t.Run("ReadFile "+tf.name, func(t *testing.T) {
			testReadFile(t, mockFS, tf.path, tf.expectedContent)
		})
	}
}

// TestErrorInjectionExactMatch exercises error injection for exact matches.
// It tests Stat, Open, and ReadDir operations with injected errors and verifies
// the correct errors are returned and the stats are updated correctly.
//
// The test also exercises ClearErrors and verifies the stats are not reset.
func TestErrorInjectionExactMatch(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"allow.txt": {Data: []byte("ok")},
		"deny.txt":  {Data: []byte("fail")},
		"dir":       {Mode: fs.ModeDir},
	})

	// Inject specific errors
	mockFS.AddStatError("deny.txt", fs.ErrPermission)
	mockFS.AddOpenError("deny.txt", mockfs.ErrTimeout) // Different error for Open
	mockFS.AddReadDirError("dir", mockfs.ErrNotExist)

	// Test Stat error
	_, err := mockFS.Stat("deny.txt")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Expected Stat permission error, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpStat: 1}, "After denied Stat")

	// Test Open error
	_, err = mockFS.Open("deny.txt")
	if !errors.Is(err, mockfs.ErrTimeout) {
		t.Errorf("Expected Open timeout error, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpStat: 1, mockfs.OpOpen: 1}, "After denied Open")

	// Test ReadDir error
	_, err = mockFS.ReadDir("dir")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected ReadDir not exist error, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpStat: 1, mockfs.OpOpen: 1, mockfs.OpReadDir: 1}, "After denied ReadDir")

	// Test allowed file
	_, err = mockFS.Stat("allow.txt")
	if err != nil {
		t.Errorf("Stat on allowed file failed: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpStat: 2, mockfs.OpOpen: 1, mockfs.OpReadDir: 1}, "After allowed Stat")

	// Clear errors and re-test denied file
	mockFS.ClearErrors()
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpStat: 2, mockfs.OpOpen: 1, mockfs.OpReadDir: 1}, "After ClearErrors") // Stats remain

	_, err = mockFS.Stat("deny.txt")
	if err != nil {
		t.Errorf("Expected Stat success after ClearErrors, got: %v", err)
	}
	finalStats := mockFS.GetStats()
	if finalStats.Count(mockfs.OpStat) != 3 {
		t.Errorf("Expected 3 stat calls finally, got %d", finalStats.Count(mockfs.OpStat))
	}
}

// TestErrorInjectionPattern exercises error injection for pattern matches.
// It tests Open and Read operations with injected errors and verifies
// the correct errors are returned and the stats are updated correctly.
//
// The test also exercises ClearErrors and ResetStats.
func TestErrorInjectionPattern(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"config.yaml": {Data: []byte("settings")},
		"data.txt":    {Data: []byte("info")},
		"log.txt":     {Data: []byte("trace")},
		"dir/log.txt": {Data: []byte("sub trace")},
	})

	// Inject pattern error for all *.txt files on Open
	err := mockFS.AddErrorPattern(mockfs.OpOpen, `\.txt$`, fs.ErrPermission, mockfs.ErrorModeAlways, 0)
	if err != nil {
		t.Fatalf("AddErrorPattern failed: %v", err)
	}
	// Inject different pattern error for log files on Read
	err = mockFS.AddErrorPattern(mockfs.OpRead, `log\.txt$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
	if err != nil {
		t.Fatalf("AddErrorPattern failed: %v", err)
	}

	// Test non-matching file (should succeed)
	_, err = mockFS.Open("config.yaml")
	if err != nil {
		t.Errorf("Open on non-matching file failed: %v", err)
	}

	// Test matching files for Open error
	_, err = mockFS.Open("data.txt")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Expected Open permission error for data.txt, got: %v", err)
	}
	_, err = mockFS.Open("log.txt")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Expected Open permission error for log.txt, got: %v", err)
	}

	// Test Read error on log.txt (needs successful Open first)
	mockFS.ClearErrors()
	err = mockFS.AddErrorPattern(mockfs.OpRead, `log\.txt$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
	if err != nil {
		t.Fatalf("AddErrorPattern failed: %v", err)
	}

	file, err := mockFS.Open("log.txt") // Should succeed now
	if err != nil {
		t.Fatalf("Open log.txt failed unexpectedly: %v", err)
	}
	defer file.Close()

	_, err = file.Read(make([]byte, 1)) // Try to read
	if !errors.Is(err, mockfs.ErrCorrupted) {
		t.Errorf("Expected Read corrupted error for log.txt, got: %v", err)
	}

	// Test Read on non-log file (data.txt) - should succeed
	mockFS.ResetStats()
	file, err = mockFS.Open("data.txt")
	if err != nil {
		t.Fatalf("Open data.txt failed: %v", err)
	}
	defer file.Close()
	_, err = file.Read(make([]byte, 1))
	if err != nil && err != io.EOF { // Allow EOF
		t.Errorf("Read on data.txt failed unexpectedly: %v", err)
	}
	stats := mockFS.GetStats()
	if stats.Count(mockfs.OpRead) != 1 {
		t.Errorf("Expected 1 read call got %d", stats.Count(mockfs.OpRead))
	}
}

// TestErrorModes exercises error injection modes.
//
// The test covers ErrorModeOnce where the first attempt fails, but subsequent
// attempts succeed. It also tests ErrorModeAfterSuccesses where the error is
// injected after a specified number of successful operations. In this case,
// the error is injected after 2 successful reads (failing on the 3rd read).
// The test verifies the correct errors are returned and the stats are updated
// correctly.
func TestErrorModes(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("abcde")},
	})

	// --- Test ErrorModeOnce ---
	mockFS.AddOpenErrorOnce("test.txt", fs.ErrPermission)

	// First attempt should fail
	_, err := mockFS.Open("test.txt")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("ModeOnce: Expected permission error on first attempt, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 1}, "ModeOnce Fail")

	// Second attempt should succeed
	file, err := mockFS.Open("test.txt")
	if err != nil {
		t.Errorf("ModeOnce: Expected success on second attempt, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 2}, "ModeOnce Success")

	// Third attempt should also succeed
	file2, err := mockFS.Open("test.txt")
	if err != nil {
		t.Errorf("ModeOnce: Expected success on third attempt, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 3}, "ModeOnce Success 2")

	if file != nil {
		file.Close()
	}
	if file2 != nil {
		file2.Close()
	}
	mockFS.ResetStats()
	mockFS.ClearErrors()

	// --- Test ErrorModeAfterSuccesses ---
	// Add error after 2 successful reads (fail on 3rd read)
	mockFS.AddReadErrorAfterN("test.txt", mockfs.ErrCorrupted, 2)

	file, err = mockFS.Open("test.txt") // Open should succeed
	if err != nil {
		t.Fatalf("ModeAfterN: Failed to open file: %v", err)
	}
	defer file.Close()
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 1}, "ModeAfterN Open")

	buf := make([]byte, 1) // Read one byte at a time

	// First read should succeed
	_, err = file.Read(buf)
	if err != nil {
		t.Errorf("ModeAfterN: Expected first read to succeed, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 1, mockfs.OpRead: 1}, "ModeAfterN Read 1 Success")

	// Second read should succeed
	_, err = file.Read(buf)
	if err != nil {
		t.Errorf("ModeAfterN: Expected second read to succeed, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 1, mockfs.OpRead: 2}, "ModeAfterN Read 2 Success")

	// Third read should fail with corruption error
	_, err = file.Read(buf)
	if !errors.Is(err, mockfs.ErrCorrupted) {
		t.Errorf("ModeAfterN: Expected corruption error on third read, got: %v", err)
	}
	// Stat check after failure - note the counter includes the failed call
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 1, mockfs.OpRead: 3}, "ModeAfterN Read 3 Fail")

	// Fourth read should also fail (ErrorModeAfterSuccesses keeps failing after trigger)
	_, err = file.Read(buf)
	if !errors.Is(err, mockfs.ErrCorrupted) {
		t.Errorf("ModeAfterN: Expected corruption error on fourth read, got: %v", err)
	}
	checkStats(t, mockFS, [mockfs.NumOperations]int{mockfs.OpOpen: 1, mockfs.OpRead: 4}, "ModeAfterN Read 4 Fail")
}

// TestStatistics exercises the statistics tracking.
//
// It performs various operations (Stat, Open, Read, ReadDir, Close) and verifies
// the correct statistics are updated. Then it resets the statistics and verifies
// they are reset correctly.
func TestStatistics(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("hello")},
		"dir":      {Mode: fs.ModeDir},
	})

	mockFS.ResetStats()
	checkStats(t, mockFS, [mockfs.NumOperations]int{}, "Initial")

	_, _ = mockFS.Stat("test.txt")
	_, _ = mockFS.Stat("dir")
	file, _ := mockFS.Open("test.txt")
	if file != nil {
		_, _ = file.Read(make([]byte, 1))
		_, _ = file.Read(make([]byte, 10)) // Second read
		_ = file.Close()
	}
	_, _ = mockFS.ReadDir("dir")

	expected := [mockfs.NumOperations]int{
		mockfs.OpStat:    2,
		mockfs.OpOpen:    1,
		mockfs.OpRead:    2,
		mockfs.OpReadDir: 1,
		mockfs.OpClose:   1,
	}
	checkStats(t, mockFS, expected, "After operations")

	mockFS.ResetStats()
	checkStats(t, mockFS, [mockfs.NumOperations]int{}, "After Reset")
}

// TestLatencySimulation exercises the latency simulation feature.
// It sets up a mockfs with a latency and then times various operations
// (Stat, Open, Read, Close) to verify that the simulated latency is
// within the expected range.
func TestLatencySimulation(t *testing.T) {
	// Don't run in parallel, timing is sensitive
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("hello")},
	}, mockfs.WithLatency(20*time.Millisecond)) // Use Option func

	minDuration := 15 * time.Millisecond  // Allow some tolerance lower
	maxDuration := 100 * time.Millisecond // Allow generous upper bound

	// Time Stat
	start := time.Now()
	_, err := mockFS.Stat("test.txt")
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if duration < minDuration || duration > maxDuration {
		t.Errorf("Stat duration (%v) outside expected range [%v, %v]", duration, minDuration, maxDuration)
	}

	// Time Open
	start = time.Now()
	file, err := mockFS.Open("test.txt")
	duration = time.Since(start)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if duration < minDuration || duration > maxDuration {
		t.Errorf("Open duration (%v) outside expected range [%v, %v]", duration, minDuration, maxDuration)
	}

	if file != nil {
		start = time.Now()
		_, err = file.Read(make([]byte, 1))
		duration = time.Since(start)
		if err != nil && err != io.EOF {
			t.Fatalf("Read failed: %v", err)
		}
		if duration < minDuration || duration > maxDuration {
			t.Errorf("Read duration (%v) outside expected range [%v, %v]", duration, minDuration, maxDuration)
		}

		// Time Close
		start = time.Now()
		err = file.Close()
		duration = time.Since(start)
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}
		if duration < minDuration || duration > maxDuration {
			t.Errorf("Close duration (%v) outside expected range [%v, %v]", duration, minDuration, maxDuration)
		}
	}
}

// setupParentFS creates a fresh parent FS with predefined errors for subfilesystem tests.
func setupParentFS() *mockfs.MockFS {
	parentFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"dir":                  {Mode: fs.ModeDir | 0755},
		"dir/file1.txt":        {Data: []byte("file1")},
		"dir/subdir":           {Mode: fs.ModeDir | 0755},
		"dir/subdir/file2.txt": {Data: []byte("file2")},
		"outside.txt":          {Data: []byte("outside")},
	})
	parentFS.AddOpenError("dir/file1.txt", mockfs.ErrTimeout)
	parentFS.AddStatError("dir/subdir/file2.txt", fs.ErrPermission)
	parentFS.AddReadDirError("dir/subdir", mockfs.ErrDiskFull)
	_ = parentFS.AddErrorPattern(mockfs.OpOpen, `^dir/`, mockfs.ErrInvalid, mockfs.ErrorModeAlways, 0)
	return parentFS
}

// expectSubFilesystemError checks for expected error (including wrapped errors) in subfilesystem tests.
func expectSubFilesystemError(t *testing.T, got, want error) {
	t.Helper()
	if errors.Is(got, want) {
		return
	}
	// Check for wrapped PathError
	var pathErr *fs.PathError
	if errors.As(got, &pathErr) && errors.Is(pathErr.Err, want) {
		return
	}
	t.Errorf("Expected error %v, got: %v", want, got)
}

// TestSubFilesystemErrorPropagation tests error propagation from parent to sub-filesystems for various operations.
func TestSubFilesystemErrorPropagation(t *testing.T) {
	t.Parallel()
	parentFS := setupParentFS()
	subFS, err := fs.Sub(parentFS, "dir")
	if err != nil {
		t.Fatalf("fs.Sub failed: %v", err)
	}

	testCases := []struct {
		name      string
		path      string
		operation func(fs.FS, string) error
		wantErr   error
	}{
		{"OpenFileWithError", "file1.txt", func(f fs.FS, p string) error { _, err := f.Open(p); return err }, mockfs.ErrTimeout},
		{"StatFileWithError", "subdir/file2.txt", func(f fs.FS, p string) error { _, err := fs.Stat(f, p); return err }, fs.ErrPermission},
		{"ReadDirWithError", "subdir", func(f fs.FS, p string) error { _, err := fs.ReadDir(f, p); return err }, mockfs.ErrDiskFull},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.operation(subFS, tc.path)
			expectSubFilesystemError(t, got, tc.wantErr)
		})
	}
}

// TestSubFilesystemBoundaryChecks tests sub-filesystem boundary checks, specifically
// the error behavior when attempting to access a file outside the sub-filesystem
// and the correctness of Stat when accessing the root of a sub-filesystem.
func TestSubFilesystemBoundaryChecks(t *testing.T) {
	t.Parallel()

	t.Run("AccessOutsideSubFS", func(t *testing.T) {
		parentFS := setupParentFS()
		subFS, err := fs.Sub(parentFS, "dir")
		if err != nil {
			t.Fatalf("fs.Sub failed: %v", err)
		}

		_, err = subFS.Open("../outside.txt")
		if err == nil {
			t.Error("Expected an error when accessing outside subFS, got nil")
		} else if !errors.Is(err, fs.ErrInvalid) && !errors.Is(err, fs.ErrNotExist) {
			t.Logf("Got expected error type: %v", err)
		}
	})

	t.Run("StatRootOfSubFS", func(t *testing.T) {
		parentFS := setupParentFS()
		subFS, err := fs.Sub(parentFS, "dir")
		if err != nil {
			t.Fatalf("fs.Sub failed: %v", err)
		}

		info, err := fs.Stat(subFS, ".")
		if err != nil {
			t.Fatalf("fs.Stat(subFS, .) failed: %v", err)
		}
		if !info.IsDir() || info.Name() != "." {
			t.Errorf("Expected directory named '.', got: %+v", info)
		}
	})
}

// TestSubFilesystemNesting tests error injection for operations on a nested
// sub-filesystem. This test ensures that injected errors are propagated to the
// correct sub-filesystem.
func TestSubFilesystemNesting(t *testing.T) {
	t.Parallel()

	parentFS := setupParentFS()
	subFS, err := fs.Sub(parentFS, "dir")
	if err != nil {
		t.Fatalf("Sub(dir) failed: %v", err)
	}
	subSubFS, err := fs.Sub(subFS, "subdir")
	if err != nil {
		t.Fatalf("Sub(subdir) failed: %v", err)
	}
	_, err = fs.Stat(subSubFS, "file2.txt")
	expectSubFilesystemError(t, err, fs.ErrPermission)
}

// TestSubFilesystemErrorCases tests Sub handling of various error cases.
// These tests cover calling Sub on a file and attempting to create a sub-filesystem
// from a non-existent directory.
func TestSubFilesystemErrorCases(t *testing.T) {
	t.Parallel()

	t.Run("SubOnAFile", func(t *testing.T) {
		parentFS := setupParentFS()
		_, err := fs.Sub(parentFS, "dir/file1.txt")
		if err == nil {
			t.Error("Expected error when calling Sub on a file, got nil")
		}
	})

	t.Run("SubOnNonExistent", func(t *testing.T) {
		parentFS := setupParentFS()
		_, err := fs.Sub(parentFS, "nonexistent")
		expectSubFilesystemError(t, err, fs.ErrNotExist)
	})
}

// TestFileManagement exercises the file management operations on a mock FS:
// AddFileString, AddFileBytes, AddDirectory, RemovePath, and MarkDirectoryNonExistent.
// It verifies correctness of file additions, removals, and directory marking.
func TestFileManagement(t *testing.T) {
	t.Parallel()

	// setupFS creates a new mock FS with a standard set of files.
	setupFS := func() *mockfs.MockFS {
		mockFS := mockfs.NewMockFS(nil) // Start empty
		mockFS.AddFileString("file.txt", "content", 0644)
		mockFS.AddFileBytes("data.bin", []byte{1, 2, 3}, 0600)
		mockFS.AddDirectory("d1", 0755)
		mockFS.AddFileString("d1/nested.txt", "nested", 0644)
		mockFS.AddDirectory("d1/d2", 0700)
		return mockFS
	}

	t.Run("VerifyAdditions", func(t *testing.T) {
		mockFS := setupFS()
		testCases := []struct {
			path      string
			isDir     bool
			size      int64
			perm      fs.FileMode
			expectErr bool
		}{
			{"file.txt", false, 7, 0644, false},
			{"data.bin", false, 3, 0600, false},
			{"d1", true, 0, 0755, false},
			{"d1/nested.txt", false, 6, 0644, false},
			{"d1/d2", true, 0, 0700, false},
			{"nonexistent", false, 0, 0, true},
		}

		for _, tc := range testCases {
			info, err := mockFS.Stat(tc.path)
			if (err != nil) != tc.expectErr {
				t.Fatalf("Stat(%q): unexpected error state: %v", tc.path, err)
			}
			if err == nil {
				if info.IsDir() != tc.isDir || info.Size() != tc.size || info.Mode().Perm() != tc.perm {
					t.Errorf("Stat(%q): mismatch: got dir=%v size=%d perm=%v, want dir=%v size=%d perm=%v",
						tc.path, info.IsDir(), info.Size(), info.Mode().Perm(), tc.isDir, tc.size, tc.perm)
				}
			}
		}
	})

	t.Run("RemoveSingleFile", func(t *testing.T) {
		mockFS := setupFS()
		mockFS.RemovePath("data.bin")
		_, err := mockFS.Stat("data.bin")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Expected data.bin to be removed, got err: %v", err)
		}
	})

	t.Run("MarkDirectoryNonExistent", func(t *testing.T) {
		mockFS := setupFS()
		mockFS.MarkDirectoryNonExistent("d1")

		pathsToCheck := []string{"d1", "d1/nested.txt", "d1/d2"}
		for _, path := range pathsToCheck {
			_, err := mockFS.Stat(path)
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Expected %q to return ErrNotExist, got: %v", path, err)
			}
		}
	})
}

// TestWriteOperations tests the Write method of the mockfs.MockFS.
// It requires enabling writes, e.g., using WithWritesEnabled.
func TestWriteOperations(t *testing.T) {
	t.Parallel()

	// setupFS creates a new FS instance with writes enabled for isolated testing.
	setupFS := func() *mockfs.MockFS {
		initialData := map[string]*mockfs.MapFile{
			"writable.txt": {Data: []byte("initial")},
			"readonly.txt": {Data: []byte("nochange")},
		}
		return mockfs.NewMockFS(initialData, mockfs.WithWritesEnabled())
	}

	tests := []struct {
		name            string
		setupFunc       func() *mockfs.MockFS
		filename        string
		writeData       []byte
		expectedBytes   int
		expectedError   error
		expectedContent string
		description     string
	}{
		{
			name:            "SuccessfulWrite",
			setupFunc:       setupFS,
			filename:        "writable.txt",
			writeData:       []byte(" new"),
			expectedBytes:   4,
			expectedError:   nil,
			expectedContent: " new",
			description:     "Should successfully write to existing file and overwrite content",
		},
		{
			name: "InjectedWriteError",
			setupFunc: func() *mockfs.MockFS {
				mockFS := setupFS()
				mockFS.AddWriteError("writable.txt", mockfs.ErrDiskFull)
				return mockFS
			},
			filename:        "writable.txt",
			writeData:       []byte(" again"),
			expectedBytes:   0,
			expectedError:   mockfs.ErrDiskFull,
			expectedContent: "initial",
			description:     "Should return error and leave content unchanged when write fails",
		},
		{
			name: "CreateFileOnWrite",
			setupFunc: func() *mockfs.MockFS {
				mockFS := setupFS()
				mockFS.AddFileString("newfile.txt", "", 0644)
				return mockFS
			},
			filename:        "newfile.txt",
			writeData:       []byte("new content"),
			expectedBytes:   11,
			expectedError:   nil,
			expectedContent: "new content",
			description:     "Should successfully write to newly created file",
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockFS := tt.setupFunc()
			file, err := mockFS.Open(tt.filename)
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}
			defer file.Close()

			writer, ok := file.(io.Writer)
			if !ok {
				t.Fatalf("File does not implement io.Writer")
			}

			n, err := writer.Write(tt.writeData)

			// Check error expectation
			if tt.expectedError != nil {
				if !errors.Is(err, tt.expectedError) {
					t.Errorf("Expected error %v, got: %v", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("Write failed: %v", err)
				}
				// Only check bytes written if no error was expected
				if n != tt.expectedBytes {
					t.Errorf("Expected Write to return %d, got %d", tt.expectedBytes, n)
				}
			}

			// Verify content
			content, err := mockFS.ReadFile(tt.filename)
			if err != nil {
				t.Fatalf("ReadFile after write failed: %v", err)
			}
			if string(content) != tt.expectedContent {
				t.Errorf("Expected content %q, got %q", tt.expectedContent, string(content))
			}
		})
	}
}

// TestMockFileStatPassThrough explicitly verifies that a Stat call on an open MockFile handle
// successfully retrieves the FileInfo from the underlying file. This covers
// the simple pass-through logic in `MockFile.Stat`.
func TestMockFileStatPassThrough(t *testing.T) {
	t.Parallel()
	now := time.Now()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("content"), ModTime: now},
	})

	file, err := mockFS.Open("file.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("MockFile.Stat() failed: %v", err)
	}

	if info.Name() != "file.txt" {
		t.Errorf("expected name %q, got %q", "file.txt", info.Name())
	}
	if info.Size() != 7 {
		t.Errorf("expected size %d, got %d", 7, info.Size())
	}
	if !info.ModTime().Equal(now) {
		t.Errorf("expected mod time %v, got %v", now, info.ModTime())
	}
}

// TestReadFileErrorPaths verifies the error handling within ReadFile.
// It ensures that errors injected into the underlying Open and Read
// operations are correctly propagated.
func TestReadFileErrorPaths(t *testing.T) {
	t.Parallel()
	errInjected := errors.New("injected error")

	testCases := []struct {
		name        string
		setupFS     func(m *mockfs.MockFS)
		expectedErr error
	}{
		{
			name: "PropagatesOpenError",
			setupFS: func(m *mockfs.MockFS) {
				m.AddOpenError("file.txt", errInjected)
			},
			expectedErr: errInjected,
		},
		{
			name: "PropagatesReadError",
			setupFS: func(m *mockfs.MockFS) {
				m.AddReadError("file.txt", errInjected)
			},
			expectedErr: errInjected,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
				"file.txt": {Data: []byte("hello")},
			})
			tc.setupFS(mockFS)

			_, err := mockFS.ReadFile("file.txt")
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("expected error %v, got %v", tc.expectedErr, err)
			}
		})
	}
}

// TestConvenienceErrorHelpers validates that the simple, single-operation error helpers
// correctly configure the filesystem to inject errors for specific actions.
func TestConvenienceErrorHelpers(t *testing.T) {
	t.Parallel()
	errInjected := errors.New("injected error")

	t.Run("AddReadError", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("hello")},
		})
		mockFS.AddReadError("file.txt", errInjected)

		f, err := mockFS.Open("file.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer f.Close()

		_, err = f.Read(make([]byte, 1))
		if !errors.Is(err, errInjected) {
			t.Errorf("expected Read error %v, got %v", errInjected, err)
		}
	})

	t.Run("AddCloseError", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("hello")},
		})
		mockFS.AddCloseError("file.txt", errInjected)

		f, err := mockFS.Open("file.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		err = f.Close()
		if !errors.Is(err, errInjected) {
			t.Errorf("expected Close error %v, got %v", errInjected, err)
		}
	})
}

// TestMarkNonExistent ensures that MarkNonExistent correctly removes a path and
// injects ErrNotExist for any subsequent operations on that path,
// without affecting other files.
func TestMarkNonExistent(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"existent.txt":  {Data: []byte("I am here")},
		"to_delete.txt": {Data: []byte("I will be gone")},
	})

	mockFS.MarkNonExistent("to_delete.txt")

	// Verify the marked file is gone and errors out
	_, err := mockFS.Stat("to_delete.txt")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected ErrNotExist for Stat, got %v", err)
	}
	_, err = mockFS.Open("to_delete.txt")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected ErrNotExist for Open, got %v", err)
	}

	// Verify other files are unaffected
	info, err := mockFS.Stat("existent.txt")
	if err != nil {
		t.Errorf("expected no error for other file, got %v", err)
	}
	if info == nil || info.Name() != "existent.txt" {
		t.Errorf("Stat on other file returned unexpected result")
	}
}

// TestClosedFileOperations verifies that all operations on a closed MockFile
// correctly return fs.ErrClosed.
func TestClosedFileOperations(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("data")},
	}, mockfs.WithWritesEnabled()) // Enable writes for Write test

	file, err := mockFS.Open("test.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Close the file to simulate closed-file operations
	if err := file.Close(); err != nil {
		t.Fatalf("Initial Close failed: %v", err)
	}

	tests := []struct {
		name string
		op   func() error
	}{
		{
			name: "ReadOnClosedFile",
			op: func() error {
				_, err := file.Read(make([]byte, 1))
				return err
			},
		},
		{
			name: "WriteOnClosedFile",
			op: func() error {
				writer, ok := file.(io.Writer)
				if !ok {
					t.Fatal("File does not implement io.Writer")
				}
				_, err := writer.Write([]byte("more"))
				return err
			},
		},
		{
			name: "StatOnClosedFile",
			op: func() error {
				_, err := file.Stat()
				return err
			},
		},
		{
			name: "CloseAlreadyClosedFile",
			op: func() error {
				return file.Close()
			},
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			if !errors.Is(err, fs.ErrClosed) {
				t.Errorf("%s: expected fs.ErrClosed, got %v", tt.name, err)
			}
		})
	}
}

// TestFileOperationsEdgeCases exercises various edge cases of the mockfs
// implementation that are not covered by the other test cases.
func TestFileOperationsEdgeCases(t *testing.T) {
	t.Parallel()

	// This subtest ensures that attempting to write to a file fails with
	// fs.ErrInvalid if the filesystem was not configured with writes enabled.
	// This covers the `if !canWrite` check in the Write method.
	t.Run("WriteWhenDisabled", func(t *testing.T) {
		// Create FS without WithWritesEnabled()
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"test.txt": {Data: []byte("data")},
		})

		file, err := mockFS.Open("test.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer file.Close()

		writer, ok := file.(io.Writer)
		if !ok {
			t.Fatal("File does not implement io.Writer")
		}

		_, err = writer.Write([]byte("some data"))
		if !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Write with writes disabled: expected fs.ErrInvalid, got %v", err)
		}
	})

	// This subtest validates that injecting an error for the Close operation
	// works as expected and that the file is still marked as closed internally
	// even if the close operation returns an error.
	t.Run("InjectedCloseError", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"test.txt": {Data: []byte("data")},
		})

		// Inject a specific error for the Close operation
		err := mockFS.AddErrorPattern(mockfs.OpClose, `^test\.txt$`, mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0)
		if err != nil {
			t.Fatalf("AddErrorPattern for OpClose failed: %v", err)
		}

		file, err := mockFS.Open("test.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		// This Close call should fail with the injected error
		err = file.Close()
		if !errors.Is(err, mockfs.ErrTimeout) {
			t.Errorf("Close with injected error: expected mockfs.ErrTimeout, got %v", err)
		}

		// Even though Close failed, the file handle should now be marked as closed internally.
		// Any subsequent operation should return fs.ErrClosed.
		_, err = file.Stat()
		if !errors.Is(err, fs.ErrClosed) {
			t.Errorf("Stat after failed Close: expected fs.ErrClosed, got %v", err)
		}
	})

	// This subtest covers the scenario where a custom write callback returns an
	// error, ensuring the error is propagated and the underlying file data
	// is not modified.
	t.Run("WriteCallbackError", func(t *testing.T) {
		customErr := errors.New("custom write callback error")

		// Create FS with a callback that always returns an error
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"test.txt": {Data: []byte("initial")},
		}, mockfs.WithWritesEnabled(), mockfs.WithWriteCallback(func(path string, data []byte) error {
			return customErr
		}))

		file, err := mockFS.Open("test.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer file.Close()

		writer, ok := file.(io.Writer)
		if !ok {
			t.Fatal("File does not implement io.Writer")
		}

		_, err = writer.Write([]byte("some data"))
		if !errors.Is(err, customErr) {
			t.Errorf("Write with failing callback: expected custom error, got %v", err)
		}

		// Verify the file content was not changed
		content, err := mockFS.ReadFile("test.txt")
		if err != nil {
			t.Fatalf("ReadFile after failed write failed: %v", err)
		}
		if string(content) != "initial" {
			t.Errorf("Expected content to be 'initial' after failed write, got %q", string(content))
		}
	})
}

// Test concurrent operations to check locking.
func TestConcurrency(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file1.txt": {Data: []byte("1")},
		"file2.txt": {Data: []byte("2")},
		"file3.txt": {Data: []byte("3")},
	}, mockfs.WithLatency(1*time.Millisecond)) // Add slight latency

	// Inject some errors that involve state changes
	mockFS.AddOpenErrorOnce("file1.txt", fs.ErrPermission)
	mockFS.AddReadErrorAfterN("file2.txt", mockfs.ErrCorrupted, 5) // Fail after 5 successes

	var wg sync.WaitGroup
	numGoroutines := 10
	numOpsPerG := 5

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < numOpsPerG; i++ {
				// Mix of operations
				_, _ = mockFS.Stat("file1.txt")
				f1, err1 := mockFS.Open("file1.txt")
				if err1 == nil {
					_, _ = io.ReadAll(f1) // Read triggers read stats/errors
					_ = f1.Close()
				}

				f2, err2 := mockFS.Open("file2.txt")
				if err2 == nil {
					_, _ = io.ReadAll(f2) // Read triggers read stats/errors
					_ = f2.Close()
				}
				_, _ = mockFS.Stat("file3.txt")
			}
		}(g)
	}

	wg.Wait()

	// Verification: It's hard to predict exact stats due to concurrency.
	// Key checks:
	// 1. No panics occurred (indicates basic lock safety).
	// 2. The ErrorModeOnce error was triggered roughly once.
	// 3. The ErrorModeAfterSuccesses error was triggered after successes.

	stats := mockFS.GetStats()
	t.Logf("Stats after concurrent run: %+v", stats)

	// Check ErrorModeOnce: mockfs.OpOpen for file1.txt should be numGoroutines * numOpsPerG.
	// We expect exactly one fs.ErrPermission error among those calls.
	// Re-running the first Open call should now succeed.
	_, err := mockFS.Open("file1.txt")
	if err != nil {
		t.Errorf("Open on file1.txt after concurrent run failed unexpectedly: %v", err)
	}

	// Check ErrorModeAfterSuccesses: mockfs.OpRead for file2.txt should be significant.
	// We expect ErrCorrupted after the 5th successful read overall.
	// Perform a few more reads to confirm it keeps failing.
	successCount := 0
	failCount := 0
	for i := 0; i < 10; i++ {
		f, ferr := mockFS.Open("file2.txt")
		if ferr != nil {
			t.Fatalf("Open file2 failed: %v", ferr)
		}
		_, rerr := f.Read(make([]byte, 1))
		_ = f.Close()
		if rerr == nil || rerr == io.EOF {
			successCount++
		} else if errors.Is(rerr, mockfs.ErrCorrupted) {
			failCount++
		} else {
			t.Fatalf("Unexpected read error on file2: %v", rerr)
		}
	}

	// We expect the first 5 reads total to succeed, and subsequent ones to fail.
	// This check after the concurrent run isn't precise, but checks the mode's persistence.
	t.Logf("Post-concurrency file2 reads: %d successes, %d failures (expecting failures after 5 total successes)", successCount, failCount)
	if failCount == 0 && stats.Count(mockfs.OpRead) > 5 {
		t.Error("Expected read failures on file2.txt after 5 successes, but none occurred in post-check")
	}

	// Check total stats are roughly correct order of magnitude
	totalOps := numGoroutines * numOpsPerG
	// Each loop does ~2 Stats, ~2 Opens, ~reads/closes depend on open success.
	if stats.Count(mockfs.OpStat) < totalOps || stats.Count(mockfs.OpOpen) < totalOps {
		t.Errorf("Stats seem too low after concurrent run: %+v", stats)
	}
}
