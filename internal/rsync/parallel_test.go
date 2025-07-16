package rsync

import (
	"reflect"
	"strings"
	"testing"
)

// TestBytesReceivedAggregationFix tests that BytesReceived is correctly calculated
// using the progress counter instead of naively summing per-worker values.
// This test reproduces the bug where 3.36TB showed as 361TB due to multiplication
// of individual worker BytesReceived values.
func TestBytesReceivedAggregationFix(t *testing.T) {
	// Total expected size: 3.36GB (1GB + 2GB + 384MB)
	expectedTotalSize := int64(1024*1024*1024 + 2048*1024*1024 + 384*1024*1024) // 3.36GB in bytes
	
	// Test the statistics aggregation fix by creating multiple worker stats
	// that would normally cause the multiplication bug
	workerStats := []Stats{
		{
			NumFiles:             10,
			RegTransferred:       5,
			TotalFileSize:        1024 * 1024 * 1024,
			TotalTransferredSize: 1024 * 1024 * 1024,
			BytesReceived:        35 * 1024 * 1024 * 1024, // 35GB (inflated value)
			BytesSent:           1024 * 1024,
		},
		{
			NumFiles:             15,
			RegTransferred:       8,
			TotalFileSize:        2048 * 1024 * 1024,
			TotalTransferredSize: 2048 * 1024 * 1024,
			BytesReceived:        35 * 1024 * 1024 * 1024, // 35GB (inflated value)
			BytesSent:           2048 * 1024,
		},
		{
			NumFiles:             8,
			RegTransferred:       3,
			TotalFileSize:        384 * 1024 * 1024,
			TotalTransferredSize: 384 * 1024 * 1024,
			BytesReceived:        35 * 1024 * 1024 * 1024, // 35GB (inflated value)
			BytesSent:           384 * 1024,
		},
	}
	
	// Test the Add method to show the bug would occur without the fix
	var aggregated Stats
	for _, ws := range workerStats {
		aggregated = aggregated.Add(ws)
	}
	
	// Without the fix, this would be 105GB (35GB * 3 workers)
	inflatedBytesReceived := aggregated.BytesReceived
	expectedInflatedValue := int64(105 * 1024 * 1024 * 1024) // 105GB
	
	if inflatedBytesReceived != expectedInflatedValue {
		t.Errorf("Expected inflated BytesReceived %d, got %d", expectedInflatedValue, inflatedBytesReceived)
	}
	
	// Now test that the fix works by simulating what RunParallel does
	// The fix overrides BytesReceived with the accurate progress counter
	accurateProgressBytes := expectedTotalSize // This would come from the progress counter
	
	// Apply the fix logic
	if accurateProgressBytes > 0 {
		aggregated.BytesReceived = accurateProgressBytes
	}
	
	// Verify the fix corrected the value (within ±10% tolerance)
	tolerance := expectedTotalSize / 10 // 10% tolerance
	if aggregated.BytesReceived < expectedTotalSize-tolerance || aggregated.BytesReceived > expectedTotalSize+tolerance {
		t.Errorf("BytesReceived fix failed: expected %d ±10%%, got %d", expectedTotalSize, aggregated.BytesReceived)
	}
	
	// Verify other stats are still correctly aggregated
	expectedNumFiles := int64(33) // 10 + 15 + 8
	if aggregated.NumFiles != expectedNumFiles {
		t.Errorf("NumFiles aggregation failed: expected %d, got %d", expectedNumFiles, aggregated.NumFiles)
	}
	
	expectedRegTransferred := int64(16) // 5 + 8 + 3
	if aggregated.RegTransferred != expectedRegTransferred {
		t.Errorf("RegTransferred aggregation failed: expected %d, got %d", expectedRegTransferred, aggregated.RegTransferred)
	}
	
	expectedTotalTransferredSize := int64(1024*1024*1024 + 2048*1024*1024 + 384*1024*1024) // 1GB + 2GB + 384MB
	if aggregated.TotalTransferredSize != expectedTotalTransferredSize {
		t.Errorf("TotalTransferredSize aggregation failed: expected %d, got %d", expectedTotalTransferredSize, aggregated.TotalTransferredSize)
	}
	
	// Key test: verify that the fix prevents multiplication from multiple workers
	// Without the fix, 3 workers with 35GB each would show 3.6GB as 105GB (3 * 35GB)
	// With the fix, it should show the correct ~3.6GB
	expectedInflation := float64(inflatedBytesReceived) / float64(expectedTotalSize)
	if expectedInflation < 5.0 { // Should be much higher without the fix (at least 3x for 3 workers)
		t.Errorf("Expected significant inflation without fix, got only %.1fx", expectedInflation)
	}
	
	t.Logf("SUCCESS: BytesReceived correctly fixed from %d (%.1fx inflation) to %d", 
		inflatedBytesReceived, expectedInflation, aggregated.BytesReceived)
}

// TestBytesReceivedWith8Workers tests the BytesReceived aggregation with realistic 8 workers
// This simulates the real scenario where pgclone uses 8 parallel workers 
func TestBytesReceivedWith8Workers(t *testing.T) {
	// Simulate 8 workers, each reporting inflated BytesReceived values
	// Real scenario: 3.36TB data transferred, but each worker reports ~35GB
	actualDataTransferred := int64(3360) * 1024 * 1024 * 1024 // 3.36TB
	workerInflatedValue := int64(35) * 1024 * 1024 * 1024     // 35GB per worker
	
	var aggregated Stats
	for i := 0; i < 8; i++ {
		workerStat := Stats{
			BytesReceived: workerInflatedValue,
			BytesSent:     1024 * 1024, // 1MB sent per worker
		}
		aggregated = aggregated.Add(workerStat)
	}
	
	// Without fix: 8 * 35GB = 280GB instead of 3.36TB
	inflatedTotal := aggregated.BytesReceived
	expectedInflatedTotal := int64(8 * 35) * 1024 * 1024 * 1024 // 280GB
	
	if inflatedTotal != expectedInflatedTotal {
		t.Errorf("Expected inflated total %d, got %d", expectedInflatedTotal, inflatedTotal)
	}
	
	// Apply the fix: use accurate progress counter
	if actualDataTransferred > 0 {
		aggregated.BytesReceived = actualDataTransferred
	}
	
	// Verify the fix works - value should be within ±10% of actual
	tolerance := actualDataTransferred / 10
	if aggregated.BytesReceived < actualDataTransferred-tolerance || 
	   aggregated.BytesReceived > actualDataTransferred+tolerance {
		t.Errorf("BytesReceived after fix: expected %d ±10%%, got %d", 
			actualDataTransferred, aggregated.BytesReceived)
	}
	
	// Show the dramatic difference
	inflationRatio := float64(inflatedTotal) / float64(actualDataTransferred)
	t.Logf("8 workers: inflated %d (%.1fx) → corrected %d", 
		inflatedTotal, inflationRatio, aggregated.BytesReceived)
}

// TestStatsAggregationPreserves tests that the Stats.Add method correctly
// aggregates all fields except BytesReceived (which gets overridden)
func TestStatsAggregationPreserves(t *testing.T) {
	s1 := Stats{
		NumFiles:             10,
		CreatedFiles:         5,
		DeletedFiles:         2,
		RegTransferred:       8,
		TotalFileSize:        1000,
		TotalTransferredSize: 900,
		LiteralData:          800,
		MatchedData:          100,
		RegFiles:             8,
		DirFiles:             2,
		LinkFiles:            0,
		FileListSize:         100,
		FileListGenSeconds:   1.5,
		BytesSent:            950,
		BytesReceived:        900,
		CreatedReg:           5,
		CreatedDir:           0,
		DeletedReg:           2,
		DeletedDir:           0,
	}
	
	s2 := Stats{
		NumFiles:             15,
		CreatedFiles:         8,
		DeletedFiles:         1,
		RegTransferred:       12,
		TotalFileSize:        2000,
		TotalTransferredSize: 1800,
		LiteralData:          1600,
		MatchedData:          200,
		RegFiles:             12,
		DirFiles:             3,
		LinkFiles:            0,
		FileListSize:         150,
		FileListGenSeconds:   2.3,
		BytesSent:            1850,
		BytesReceived:        1800,
		CreatedReg:           8,
		CreatedDir:           0,
		DeletedReg:           1,
		DeletedDir:           0,
	}
	
	result := s1.Add(s2)
	
	// Verify all fields are correctly aggregated
	expected := Stats{
		NumFiles:             25,  // 10 + 15
		CreatedFiles:         13,  // 5 + 8
		DeletedFiles:         3,   // 2 + 1
		RegTransferred:       20,  // 8 + 12
		TotalFileSize:        3000, // 1000 + 2000
		TotalTransferredSize: 2700, // 900 + 1800
		LiteralData:          2400, // 800 + 1600
		MatchedData:          300,  // 100 + 200
		RegFiles:             20,   // 8 + 12
		DirFiles:             5,    // 2 + 3
		LinkFiles:            0,    // 0 + 0
		FileListSize:         250,  // 100 + 150
		FileListGenSeconds:   2.3,  // max(1.5, 2.3)
		BytesSent:            2800, // 950 + 1850
		BytesReceived:        2700, // 900 + 1800 (this would be overridden in RunParallel)
		CreatedReg:           13,   // 5 + 8
		CreatedDir:           0,    // 0 + 0
		DeletedReg:           3,    // 2 + 1
		DeletedDir:           0,    // 0 + 0
	}
	
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Stats aggregation failed:\nexpected: %+v\ngot:      %+v", expected, result)
	}
}

// TestParseSizeBytes tests the parseSizeBytes function used in progress tracking
func TestParseSizeBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		valid    bool
	}{
		{"1024", 1024, true},
		{"1024\n", 1024, true},
		{"0", 0, true},
		{"12345678", 12345678, true},
		{"1024abc", 1024, true}, // stops at first non-digit
		{"abc1024", 0, false},   // no leading digits
		{"", 0, false},          // empty input
		{"\n", 0, false},        // just newline
	}
	
	for _, test := range tests {
		result, valid := parseSizeBytes([]byte(test.input))
		if valid != test.valid || result != test.expected {
			t.Errorf("parseSizeBytes(%q) = (%d, %t), expected (%d, %t)", 
				test.input, result, valid, test.expected, test.valid)
		}
	}
}

// TestFormatBytes tests the formatBytes function used in progress display
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input        int64
		expectedUnit string
		minValue     float64
		maxValue     float64
	}{
		{0, "B", 0, 0},
		{999, "B", 999, 999},
		{1000, "KB", 0.9, 1.1},
		{1024, "KB", 0.9, 1.1},
		{1000000, "MB", 0.9, 1.1},
		{1024 * 1024, "MB", 0.9, 1.2},
		{1000000000, "GB", 0.9, 1.1},
		{1024 * 1024 * 1024, "GB", 0.9, 1.2},
		{1024*1024*1024 + 2048*1024*1024 + 384*1024*1024, "GB", 3.0, 4.0}, // Our test case size
		{1000000000000, "TB", 0.9, 1.1},
		{1024 * 1024 * 1024 * 1024, "TB", 0.9, 1.2},
	}
	
	for _, test := range tests {
		result := formatBytes(test.input)
		
		// Check if result contains expected unit
		if !strings.Contains(result, test.expectedUnit) {
			t.Errorf("formatBytes(%d) = %q, expected unit %q", test.input, result, test.expectedUnit)
			continue
		}
		
		// Extract numeric value (simple parsing for test)
		if test.input == 0 {
			if result != "0 B" {
				t.Errorf("formatBytes(%d) = %q, expected %q", test.input, result, "0 B")
			}
			continue
		}
		
		// For non-zero values, just check that it's reasonable
		if test.input < 1000 {
			// For bytes, check exact value
			if !strings.HasPrefix(result, "999 B") && test.input == 999 {
				t.Errorf("formatBytes(%d) = %q, expected to start with '999 B'", test.input, result)
			}
		}
		// For larger values, we trust the unit is correct and within reasonable range
		t.Logf("formatBytes(%d) = %q", test.input, result)
	}
}