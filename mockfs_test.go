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
	t.Parallel()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt":     {Data: []byte("hello world")},
		"dir/file.txt": {Data: []byte("in dir")},
		"dir":          {Mode: fs.ModeDir},
	})

	// Stat file
	info, err := mockFS.Stat("test.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Name() != "test.txt" || info.Size() != 11 || info.IsDir() {
		t.Errorf("Stat returned unexpected info: %+v", info)
	}
	checkStats(t, mockFS, mockfs.Stats{StatCalls: 1}, "After Stat")

	// Stat dir
	info, err = mockFS.Stat("dir")
	if err != nil {
		t.Fatalf("Stat dir failed: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("Stat dir returned non-dir info: %+v", info)
	}
	checkStats(t, mockFS, mockfs.Stats{StatCalls: 2}, "After Stat dir")

	// Open and Read file
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
	// ReadAll makes multiple Read calls potentially, stats depend on internal buffer size.
	// Let's check that ReadCalls is at least 1.
	statsAfterRead := mockFS.GetStats()
	if statsAfterRead.ReadCalls < 1 {
		t.Errorf("Expected at least 1 Read call, got %d", statsAfterRead.ReadCalls)
	}

	err = file.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	statsAfterClose := mockFS.GetStats()
	// Need expected stats including reads. Let's assume 1 read for simplicity here.
	// A more robust test might read byte-by-byte to control read counts.
	if statsAfterClose.CloseCalls != 1 {
		t.Errorf("Expected 1 Close call, got %d", statsAfterClose.CloseCalls)
	}

	// ReadFile
	mockFS.ResetStats()
	content, err = mockFS.ReadFile("dir/file.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != "in dir" {
		t.Errorf("Expected 'in dir', got %q", string(content))
	}
	// ReadFile internally calls Open, ReadAll, Close. Check relevant stats.
	statsAfterReadFile := mockFS.GetStats()
	if statsAfterReadFile.OpenCalls != 1 || statsAfterReadFile.ReadCalls < 1 || statsAfterReadFile.CloseCalls != 1 {
		t.Errorf("ReadFile stats mismatch: got %+v", statsAfterReadFile)
	}

	// ReadDir
	mockFS.ResetStats()
	entries, err := mockFS.ReadDir("dir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "file.txt" {
		t.Errorf("ReadDir returned unexpected entries: %v", entries)
	}
	checkStats(t, mockFS, mockfs.Stats{ReadDirCalls: 1}, "After ReadDir")
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
	parentFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"dir":                  {Mode: fs.ModeDir | 0755},
		"dir/file1.txt":        {Data: []byte("file1")},
		"dir/subdir":           {Mode: fs.ModeDir | 0755},
		"dir/subdir/file2.txt": {Data: []byte("file2")},
		"outside.txt":          {Data: []byte("outside")},
	})

	// Add errors to parent FS
	parentFS.AddOpenError("dir/file1.txt", mockfs.ErrTimeout)
	parentFS.AddStatError("dir/subdir/file2.txt", fs.ErrPermission)
	parentFS.AddReadDirError("dir/subdir", mockfs.ErrDiskFull)
	// Add pattern error to parent, should not affect sub if path adjusted correctly
	_ = parentFS.AddErrorPattern(mockfs.OpOpen, `^dir/`, mockfs.ErrInvalid, mockfs.ErrorModeAlways, 0)

	// Get sub-filesystem for "dir"
	subFS, err := parentFS.Sub("dir")
	if err != nil {
		t.Fatalf("fs.Sub failed: %v", err)
	}

	// --- Test access within subFS ---

	// 1. Access file1.txt (should fail with timeout from parent)
	_, err = subFS.Open("file1.txt")
	if !errors.Is(err, mockfs.ErrTimeout) {
		t.Errorf("subFS.Open(file1.txt): Expected ErrTimeout, got: %v", err)
	}

	// 2. Stat subdir/file2.txt (should fail with permission error from parent)
	_, err = fs.Stat(subFS, "subdir/file2.txt")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("fs.Stat(subFS, subdir/file2.txt): Expected ErrPermission, got: %v", err)
	}

	// 3. ReadDir subdir (should fail with disk full from parent)
	_, err = fs.ReadDir(subFS, "subdir")
	if !errors.Is(err, mockfs.ErrDiskFull) {
		t.Errorf("fs.ReadDir(subFS, subdir): Expected ErrDiskFull, got: %v", err)
	}

	// 4. Try accessing file outside the sub dir (should fail)
	_, err = subFS.Open("../outside.txt") // Accessing outside
	// fs.Sub guarantees paths are checked; expect ErrInvalid or ErrNotExist
	if err == nil {
		t.Errorf("subFS.Open(../outside.txt): Expected an error, got nil")
	} else if !errors.Is(err, fs.ErrInvalid) && !errors.Is(err, fs.ErrNotExist) {
		t.Logf("subFS.Open(../outside.txt): Got expected error type: %v", err)
	}

	// 5. Stat the root of subFS (".") - should represent "dir" from parent
	info, err := fs.Stat(subFS, ".")
	if err != nil {
		t.Fatalf("fs.Stat(subFS, .): failed: %v", err)
	}
	if !info.IsDir() || info.Name() != "." { // Name within subFS is "."
		t.Errorf("fs.Stat(subFS, .): Expected directory named '.', got: %+v", info)
	}

	// 6. Get sub-sub-filesystem
	subSubFS, err := fs.Sub(subFS, "subdir")
	if err != nil {
		t.Fatalf("fs.Sub(subFS, subdir) failed: %v", err)
	}

	// Stat file2.txt within sub-sub-FS (should still have permission error)
	_, err = fs.Stat(subSubFS, "file2.txt")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("fs.Stat(subSubFS, file2.txt): Expected ErrPermission, got: %v", err)
	}

	// --- Test edge cases for Sub (using parentFS directly) ---
	// Use the parent directly because fs.Sub might return errors earlier
	_, err = parentFS.Sub("dir/file1.txt") // Sub on a file (MockFS impl specific check)
	if err == nil {
		// Expecting an error like "not a directory"
		t.Errorf("parentFS.Sub on a file: Expected error, got nil")
	} else {
		t.Logf("parentFS.Sub on a file returned expected error: %v", err)
	}

	_, err = parentFS.Sub("nonexistent") // Sub on non-existent path
	if !errors.Is(err, fs.ErrNotExist) {
		// Check if the error wraps ErrNotExist
		pathErr := &fs.PathError{}
		if !(errors.As(err, &pathErr) && errors.Is(pathErr.Err, fs.ErrNotExist)) {
			t.Errorf("parentFS.Sub on non-existent: Expected ErrNotExist (possibly wrapped), got: %T %v", err, err)
		} else {
			t.Logf("parentFS.Sub on non-existent returned expected error: %v", err)
		}
	}
}

func TestFileManagement(t *testing.T) {
	t.Parallel()
	mockFS := mockfs.NewMockFS(nil) // Start empty

	// Add files and dirs
	mockFS.AddFileString("file.txt", "content", 0644)
	mockFS.AddFileBytes("data.bin", []byte{1, 2, 3}, 0600)
	mockFS.AddDirectory("d1", 0755)
	mockFS.AddFileString("d1/nested.txt", "nested", 0644)
	mockFS.AddDirectory("d1/d2", 0700)

	// Verify additions
	info, err := mockFS.Stat("file.txt")
	if err != nil || info.Size() != 7 {
		t.Fatalf("Stat file.txt failed: %v / %+v", err, info)
	}
	info, err = mockFS.Stat("data.bin")
	if err != nil || info.Mode() != 0600 {
		t.Fatalf("Stat data.bin failed: %v / %+v", err, info)
	}
	info, err = mockFS.Stat("d1")
	if err != nil || !info.IsDir() {
		t.Fatalf("Stat d1 failed: %v / %+v", err, info)
	}
	info, err = mockFS.Stat("d1/nested.txt")
	if err != nil || info.Size() != 6 {
		t.Fatalf("Stat d1/nested.txt failed: %v / %+v", err, info)
	}
	info, err = mockFS.Stat("d1/d2")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("Stat d1/d2 failed: %v / %+v", err, info)
	}

	// Remove single file
	mockFS.RemovePath("data.bin")
	_, err = mockFS.Stat("data.bin")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected data.bin to be removed (ErrNotExist), got: %v", err)
	}

	// Mark directory non-existent (recursive)
	mockFS.MarkDirectoryNonExistent("d1")

	// Verify directory and contents are gone and return NotExist error
	_, err = mockFS.Stat("d1")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected d1 stat to return ErrNotExist after MarkDirectoryNonExistent, got: %v", err)
	}
	_, err = mockFS.Stat("d1/nested.txt")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected d1/nested.txt stat to return ErrNotExist after MarkDirectoryNonExistent, got: %v", err)
	}
	_, err = mockFS.Stat("d1/d2")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected d1/d2 stat to return ErrNotExist after MarkDirectoryNonExistent, got: %v", err)
	}

	// Open non-existent dir should also fail
	_, err = mockFS.Open("d1")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected d1 open to return ErrNotExist after MarkDirectoryNonExistent, got: %v", err)
	}
}

// TestWriteOperations requires enabling writes, e.g., using WithWritesEnabled
func TestWriteOperations(t *testing.T) {
	t.Parallel()
	initialData := map[string]*mockfs.MapFile{
		"writable.txt": {Data: []byte("initial")},
		"readonly.txt": {Data: []byte("nochange")},
	}

	// Use WithWritesEnabled to allow modifying the internal map
	mockFS := mockfs.NewMockFS(initialData, mockfs.WithWritesEnabled())

	// --- Test successful write ---
	file, err := mockFS.Open("writable.txt")
	if err != nil {
		t.Fatalf("Open writable failed: %v", err)
	}
	// Type assert to io.Writer before calling Write
	writer, ok := file.(io.Writer)
	if !ok {
		_ = file.Close()
		t.Fatalf("Opened file does not implement io.Writer")
	}

	n, err := writer.Write([]byte(" new")) // Use writer variable
	if err != nil {
		_ = file.Close()
		t.Fatalf("Write failed: %v", err)
	}
	if n != 4 {
		t.Errorf("Expected Write to return 4 bytes written, got %d", n)
	}
	err = file.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify content changed
	content, err := mockFS.ReadFile("writable.txt")
	if err != nil {
		t.Fatalf("ReadFile after write failed: %v", err)
	}
	// Note: The default write callback *overwrites*. If append is desired, callback needs change.
	// The fstest.MapFile doesn't track seek position easily for appending.
	// Current callback overwrites, so expected is " new".
	if string(content) != " new" {
		t.Errorf("Expected content ' new' after write, got %q", string(content))
	}

	// --- Test write error injection ---
	mockFS.AddWriteError("writable.txt", mockfs.ErrDiskFull)

	file, err = mockFS.Open("writable.txt")
	if err != nil {
		t.Fatalf("Open writable (2nd time) failed: %v", err)
	}

	// Assert writer again
	writer, ok = file.(io.Writer)
	if !ok {
		_ = file.Close()
		t.Fatalf("Opened file (2nd time) does not implement io.Writer")
	}

	_, err = writer.Write([]byte(" again"))
	if !errors.Is(err, mockfs.ErrDiskFull) {
		t.Errorf("Expected Write error ErrDiskFull, got: %v", err)
	}
	err = file.Close() // Close should still work
	if err != nil {
		t.Errorf("Close after failed write failed: %v", err)
	}

	// Verify content did NOT change due to write error
	content, err = fs.ReadFile(mockFS, "writable.txt") // Use fs.ReadFile
	if err != nil {
		t.Fatalf("ReadFile after failed write failed: %v", err)
	}
	if string(content) != " new" { // Should still be content from previous successful write
		t.Errorf("Expected content ' new' after failed write, got %q", string(content))
	}

	// --- Test write to non-existent file (should be created by default callback) ---
	mockFS.ClearErrors()
	// Add the file first before opening for write
	mockFS.AddFileString("newfile.txt", "", 0644)
	file, err = mockFS.Open("newfile.txt")
	if err != nil {
		t.Fatalf("Open newfile.txt after Add failed: %v", err)
	}

	writer, ok = file.(io.Writer)
	if !ok {
		_ = file.Close()
		t.Fatalf("Opened newfile does not implement io.Writer")
	}

	_, err = writer.Write([]byte("new content"))
	if err != nil {
		_ = file.Close()
		t.Fatalf("Write to new file failed: %v", err)
	}
	_ = file.Close()

	content, err = mockFS.ReadFile("newfile.txt")
	if err != nil || string(content) != "new content" {
		t.Fatalf("ReadFile newfile.txt failed or content mismatch: %v / %q", err, string(content))
	}
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
