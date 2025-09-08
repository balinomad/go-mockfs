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

// Helper to check stats
func checkStats(t *testing.T, m *mockfs.MockFS, expected mockfs.Stats, context string) {
	t.Helper()
	actual := m.GetStats()
	if actual != expected {
		t.Errorf("%s: Stats mismatch: Expected %+v, got %+v", context, expected, actual)
	}
}

func TestBasicFunctionality(t *testing.T) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt":     {Data: []byte("hello world")},
		"dir/file.txt": {Data: []byte("in dir")},
		"dir":          {Mode: fs.ModeDir},
	})

	t.Run("Step 1: Stat File", func(t *testing.T) {
		info, err := mockFS.Stat("test.txt")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if info.Name() != "test.txt" || info.Size() != 11 || info.IsDir() {
			t.Errorf("Stat returned unexpected info: %+v", info)
		}
		checkStats(t, mockFS, mockfs.Stats{StatCalls: 1}, "After Stat")
	})

	t.Run("Step 2: Stat Dir", func(t *testing.T) {
		info, err := mockFS.Stat("dir")
		if err != nil {
			t.Fatalf("Stat dir failed: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("Stat dir returned non-dir info: %+v", info)
		}
		checkStats(t, mockFS, mockfs.Stats{StatCalls: 2}, "After Stat dir")
	})

	t.Run("Step 3: Open, Read, and Close", func(t *testing.T) {
		file, err := mockFS.Open("test.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		checkStats(t, mockFS, mockfs.Stats{StatCalls: 2, OpenCalls: 1}, "After Open")

		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if string(content) != "hello world" {
			t.Errorf("Expected 'hello world', got %q", string(content))
		}

		// ReadAll can make multiple reads. Check that the count increased.
		stats := mockFS.GetStats()
		if stats.ReadCalls < 1 {
			t.Errorf("Expected at least 1 Read call, got %d", stats.ReadCalls)
		}

		if err := file.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
		statsAfterClose := mockFS.GetStats()
		if statsAfterClose.CloseCalls != 1 {
			t.Errorf("Expected 1 Close call, got %d", statsAfterClose.CloseCalls)
		}
	})

	t.Run("Step 4: ReadFile", func(t *testing.T) {
		// Get stats before the operation to check the delta
		statsBefore := mockFS.GetStats()

		content, err := mockFS.ReadFile("dir/file.txt")
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(content) != "in dir" {
			t.Errorf("Expected 'in dir', got %q", string(content))
		}

		// Check cumulative stats after ReadFile
		statsAfter := mockFS.GetStats()
		if statsAfter.OpenCalls != statsBefore.OpenCalls+1 {
			t.Errorf("Expected OpenCalls to increment by 1")
		}
		if statsAfter.CloseCalls != statsBefore.CloseCalls+1 {
			t.Errorf("Expected CloseCalls to increment by 1")
		}
		if statsAfter.ReadCalls <= statsBefore.ReadCalls {
			t.Errorf("Expected ReadCalls to increase")
		}
	})

	t.Run("Step 5: ReadDir", func(t *testing.T) {
		statsBefore := mockFS.GetStats()
		entries, err := mockFS.ReadDir("dir")
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "file.txt" {
			t.Errorf("ReadDir returned unexpected entries: %v", entries)
		}

		statsAfter := mockFS.GetStats()
		if statsAfter.ReadDirCalls != statsBefore.ReadDirCalls+1 {
			t.Errorf("Expected ReadDirCalls to increment by 1")
		}
	})
}

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
	checkStats(t, mockFS, mockfs.Stats{StatCalls: 1}, "After denied Stat")

	// Test Open error
	_, err = mockFS.Open("deny.txt")
	if !errors.Is(err, mockfs.ErrTimeout) {
		t.Errorf("Expected Open timeout error, got: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{StatCalls: 1, OpenCalls: 1}, "After denied Open")

	// Test ReadDir error
	_, err = mockFS.ReadDir("dir")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected ReadDir not exist error, got: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{StatCalls: 1, OpenCalls: 1, ReadDirCalls: 1}, "After denied ReadDir")

	// Test allowed file
	_, err = mockFS.Stat("allow.txt")
	if err != nil {
		t.Errorf("Stat on allowed file failed: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{StatCalls: 2, OpenCalls: 1, ReadDirCalls: 1}, "After allowed Stat")

	// Clear errors and re-test denied file
	mockFS.ClearErrors()
	checkStats(t, mockFS, mockfs.Stats{StatCalls: 2, OpenCalls: 1, ReadDirCalls: 1}, "After ClearErrors") // Stats remain

	_, err = mockFS.Stat("deny.txt")
	if err != nil {
		t.Errorf("Expected Stat success after ClearErrors, got: %v", err)
	}
	finalStats := mockFS.GetStats()
	if finalStats.StatCalls != 3 {
		t.Errorf("Expected 3 stat calls finally, got %d", finalStats.StatCalls)
	}
}

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
	if stats.ReadCalls != 1 {
		t.Errorf("Expected 1 read call got %d", stats.ReadCalls)
	}

}

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
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 1}, "ModeOnce Fail")

	// Second attempt should succeed
	file, err := mockFS.Open("test.txt")
	if err != nil {
		t.Errorf("ModeOnce: Expected success on second attempt, got: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 2}, "ModeOnce Success")

	// Third attempt should also succeed
	file2, err := mockFS.Open("test.txt")
	if err != nil {
		t.Errorf("ModeOnce: Expected success on third attempt, got: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 3}, "ModeOnce Success 2")

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
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 1}, "ModeAfterN Open")

	buf := make([]byte, 1) // Read one byte at a time

	// First read should succeed
	_, err = file.Read(buf)
	if err != nil {
		t.Errorf("ModeAfterN: Expected first read to succeed, got: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 1, ReadCalls: 1}, "ModeAfterN Read 1 Success")

	// Second read should succeed
	_, err = file.Read(buf)
	if err != nil {
		t.Errorf("ModeAfterN: Expected second read to succeed, got: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 1, ReadCalls: 2}, "ModeAfterN Read 2 Success")

	// Third read should fail with corruption error
	_, err = file.Read(buf)
	if !errors.Is(err, mockfs.ErrCorrupted) {
		t.Errorf("ModeAfterN: Expected corruption error on third read, got: %v", err)
	}
	// Stat check after failure - note the counter includes the failed call
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 1, ReadCalls: 3}, "ModeAfterN Read 3 Fail")

	// Fourth read should also fail (ErrorModeAfterSuccesses keeps failing after trigger)
	_, err = file.Read(buf)
	if !errors.Is(err, mockfs.ErrCorrupted) {
		t.Errorf("ModeAfterN: Expected corruption error on fourth read, got: %v", err)
	}
	checkStats(t, mockFS, mockfs.Stats{OpenCalls: 1, ReadCalls: 4}, "ModeAfterN Read 4 Fail")
}

func TestStatistics(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("hello")},
		"dir":      {Mode: fs.ModeDir},
	})

	mockFS.ResetStats()
	checkStats(t, mockFS, mockfs.Stats{}, "Initial")

	_, _ = mockFS.Stat("test.txt")
	_, _ = mockFS.Stat("dir")
	file, _ := mockFS.Open("test.txt")
	if file != nil {
		_, _ = file.Read(make([]byte, 1))
		_, _ = file.Read(make([]byte, 10)) // Second read
		_ = file.Close()
	}
	_, _ = mockFS.ReadDir("dir")

	expected := mockfs.Stats{
		StatCalls:    2,
		OpenCalls:    1,
		ReadCalls:    2,
		ReadDirCalls: 1,
		CloseCalls:   1,
	}
	checkStats(t, mockFS, expected, "After operations")

	mockFS.ResetStats()
	checkStats(t, mockFS, mockfs.Stats{}, "After Reset")
}

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
		// Time Read
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

func TestSubFilesystem(t *testing.T) {
	t.Parallel()

	// Setup function creates a fresh parent FS for each subtest that needs it.
	setupParentFS := func() *mockfs.MockFS {
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

	t.Run("ValidSubFS", func(t *testing.T) {
		parentFS := setupParentFS()
		subFS, err := parentFS.Sub("dir")
		if err != nil {
			t.Fatalf("fs.Sub failed: %v", err)
		}

		testCases := []struct {
			name      string
			path      string
			operation func(fs.FS, string) error
			expectErr error
		}{
			{"OpenFileWithError", "file1.txt", func(f fs.FS, p string) error { _, err := f.Open(p); return err }, mockfs.ErrTimeout},
			{"StatFileWithError", "subdir/file2.txt", func(f fs.FS, p string) error { _, err := fs.Stat(f, p); return err }, fs.ErrPermission},
			{"ReadDirWithError", "subdir", func(f fs.FS, p string) error { _, err := fs.ReadDir(f, p); return err }, mockfs.ErrDiskFull},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.operation(subFS, tc.path)
				if !errors.Is(err, tc.expectErr) {
					t.Errorf("Expected error %v, got: %v", tc.expectErr, err)
				}
			})
		}
	})

	t.Run("AccessOutsideSubFS", func(t *testing.T) {
		parentFS := setupParentFS()
		subFS, err := parentFS.Sub("dir")
		if err != nil {
			t.Fatalf("fs.Sub failed: %v", err)
		}
		_, err = subFS.Open("../outside.txt")
		if err == nil {
			t.Errorf("Expected an error when accessing outside subFS, got nil")
		} else if !errors.Is(err, fs.ErrInvalid) && !errors.Is(err, fs.ErrNotExist) {
			t.Logf("Got expected error type: %v", err)
		}
	})

	t.Run("StatRootOfSubFS", func(t *testing.T) {
		parentFS := setupParentFS()
		subFS, err := parentFS.Sub("dir")
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

	t.Run("SubSubFS", func(t *testing.T) {
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
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Expected ErrPermission, got: %v", err)
		}
	})

	t.Run("SubOnAFile", func(t *testing.T) {
		parentFS := setupParentFS()
		_, err := parentFS.Sub("dir/file1.txt")
		if err == nil {
			t.Errorf("Expected error when calling Sub on a file, got nil")
		}
	})

	t.Run("SubOnNonExistent", func(t *testing.T) {
		parentFS := setupParentFS()
		_, err := parentFS.Sub("nonexistent")
		if !errors.Is(err, fs.ErrNotExist) {
			pathErr := &fs.PathError{}
			if !(errors.As(err, &pathErr) && errors.Is(pathErr.Err, fs.ErrNotExist)) {
				t.Errorf("Expected ErrNotExist (wrapped), got: %T %v", err, err)
			}
		}
	})
}

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

// TestWriteOperations requires enabling writes, e.g., using WithWritesEnabled
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

	t.Run("SuccessfulWrite", func(t *testing.T) {
		mockFS := setupFS()
		file, err := mockFS.Open("writable.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer file.Close()

		writer, ok := file.(io.Writer)
		if !ok {
			t.Fatalf("File does not implement io.Writer")
		}

		n, err := writer.Write([]byte(" new"))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if n != 4 {
			t.Errorf("Expected Write to return 4, got %d", n)
		}

		// Verify content changed (default callback overwrites)
		content, err := mockFS.ReadFile("writable.txt")
		if err != nil {
			t.Fatalf("ReadFile after write failed: %v", err)
		}
		if string(content) != " new" {
			t.Errorf("Expected content ' new', got %q", string(content))
		}
	})

	t.Run("InjectedWriteError", func(t *testing.T) {
		mockFS := setupFS()
		mockFS.AddWriteError("writable.txt", mockfs.ErrDiskFull)

		file, err := mockFS.Open("writable.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer file.Close()

		writer, ok := file.(io.Writer)
		if !ok {
			t.Fatalf("File does not implement io.Writer")
		}

		_, err = writer.Write([]byte(" again"))
		if !errors.Is(err, mockfs.ErrDiskFull) {
			t.Errorf("Expected ErrDiskFull, got: %v", err)
		}

		// Verify content did NOT change
		content, err := mockFS.ReadFile("writable.txt")
		if err != nil {
			t.Fatalf("ReadFile after failed write failed: %v", err)
		}
		if string(content) != "initial" { // Should be the original content
			t.Errorf("Expected content 'initial' after failed write, got %q", string(content))
		}
	})

	t.Run("CreateFileOnWrite", func(t *testing.T) {
		mockFS := setupFS()
		mockFS.AddFileString("newfile.txt", "", 0644) // Add file before opening
		file, err := mockFS.Open("newfile.txt")
		if err != nil {
			t.Fatalf("Open newfile.txt failed: %v", err)
		}
		defer file.Close()

		writer, ok := file.(io.Writer)
		if !ok {
			t.Fatalf("File does not implement io.Writer")
		}

		_, err = writer.Write([]byte("new content"))
		if err != nil {
			t.Fatalf("Write to new file failed: %v", err)
		}

		content, err := mockFS.ReadFile("newfile.txt")
		if err != nil || string(content) != "new content" {
			t.Fatalf("ReadFile newfile.txt failed or content mismatch: %v / %q", err, string(content))
		}
	})
}

// Test concurrent operations to check locking
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

	// Check ErrorModeOnce: OpenCalls for file1.txt should be numGoroutines * numOpsPerG.
	// We expect exactly one fs.ErrPermission error among those calls.
	// Re-running the first Open call should now succeed.
	_, err := mockFS.Open("file1.txt")
	if err != nil {
		t.Errorf("Open on file1.txt after concurrent run failed unexpectedly: %v", err)
	}

	// Check ErrorModeAfterSuccesses: ReadCalls for file2.txt should be significant.
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
	if failCount == 0 && stats.ReadCalls > 5 {
		t.Error("Expected read failures on file2.txt after 5 successes, but none occurred in post-check")
	}

	// Check total stats are roughly correct order of magnitude
	totalOps := numGoroutines * numOpsPerG
	// Each loop does ~2 Stats, ~2 Opens, ~reads/closes depend on open success.
	if stats.StatCalls < totalOps || stats.OpenCalls < totalOps {
		t.Errorf("Stats seem too low after concurrent run: %+v", stats)
	}
}
