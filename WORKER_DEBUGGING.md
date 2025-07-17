# Worker Debugging Guide

## Overview

The parallel rsync implementation now includes comprehensive per-worker debugging to help diagnose performance issues and load imbalances.

## Log Messages

### Worker Startup
```
level=INFO msg="worker starting" worker_id=0 module=base file_count=1234 total_size_gb=12.5 largest_file_gb=2.1 smallest_file_mb=512 avg_file_mb=10.4
```

**Fields:**
- `worker_id`: Unique worker identifier (0-based)
- `module`: rsync module being processed (base, pgdata, spc_123, etc.)
- `file_count`: Number of files assigned to this worker
- `total_size_gb`: Total size of all files assigned to this worker
- `largest_file_gb`: Size of the largest file assigned to this worker
- `smallest_file_mb`: Size of the smallest file assigned to this worker
- `avg_file_mb`: Average file size for this worker

### Worker Completion (Success)
```
level=INFO msg="worker completed" worker_id=0 module=base status=success total_time_sec=120.5 rsync_time_sec=118.2 setup_time_sec=2.3 files_processed=1234 files_transferred=567 bytes_received_gb=11.8 bytes_sent_mb=45.2 transfer_rate_mbps=102.3 literal_data_gb=8.5 matched_data_gb=3.3 initial_file_count=1234 initial_total_size_gb=12.5 initial_largest_file_gb=2.1 initial_smallest_file_mb=512 initial_avg_file_mb=10.4
```

**Fields:**
- `status`: Always "success" for completed workers
- `total_time_sec`: Total time from worker start to completion
- `rsync_time_sec`: Time spent in rsync process execution
- `setup_time_sec`: Time spent on setup/cleanup (total_time - rsync_time)
- `files_processed`: Number of files processed by rsync
- `files_transferred`: Number of files actually transferred (not skipped)
- `bytes_received_gb`: Actual bytes received by this worker
- `bytes_sent_mb`: Bytes sent by this worker (protocol overhead)
- `transfer_rate_mbps`: Transfer rate in MB/s (bytes_received / total_time)
- `literal_data_gb`: New data transferred (not matched locally)
- `matched_data_gb`: Data matched locally (not transferred)
- `initial_*`: Original file assignment statistics for comparison

### Worker Failure (rsync error)
```
level=ERROR msg="worker failed" worker_id=0 module=base status=rsync_error error="rsync: connection unexpectedly closed" total_time_sec=45.2 rsync_time_sec=43.1 initial_file_count=1234 initial_total_size_gb=12.5
```

### Worker Failure (awk error)
```
level=ERROR msg="worker failed" worker_id=0 module=base status=awk_error error="awk errors: [exit status 1]" total_time_sec=120.5 rsync_time_sec=118.2 initial_file_count=1234 initial_total_size_gb=12.5
```

### Worker Cancellation
```
level=WARN msg="worker cancelled" worker_id=0 module=base status=context_cancelled total_time_sec=30.2 initial_file_count=1234 initial_total_size_gb=12.5
```

### Module Summary
```
level=INFO msg="all workers completed" module=base total_workers=96 total_bytes_received_gb=1024.5 total_files_processed=50000 total_files_transferred=25000 total_time_sec=300.2
```

## Debugging Common Issues

### Identifying Slow Workers

1. **Look for workers with high `total_time_sec`** compared to others
2. **Check `transfer_rate_mbps`** - slow workers will have low rates
3. **Compare `rsync_time_sec` vs `setup_time_sec`** - high setup time indicates system bottlenecks

### Analyzing Load Imbalance

1. **Compare `initial_total_size_gb`** across workers - should be similar
2. **Check `largest_file_gb`** - workers with very large files may take longer
3. **Look at `file_count`** - workers with many small files may be slower due to overhead

### Network/I/O Issues

1. **Low `transfer_rate_mbps`** across all workers indicates network bottleneck
2. **High `literal_data_gb` vs `matched_data_gb` ratio** indicates mostly new data
3. **Long `rsync_time_sec` with small `bytes_received_gb`** suggests slow I/O

### File Distribution Issues

1. **High variance in `initial_total_size_gb`** indicates distribution algorithm problems
2. **Workers with single large files** (`file_count=1`, high `largest_file_gb`) may be bottlenecks
3. **Workers with many tiny files** (high `file_count`, low `avg_file_mb`) may have high overhead

## Example Analysis

```bash
# Extract worker completion times
grep "worker completed" /tmp/pgclone.out | jq -r '.total_time_sec' | sort -n

# Find slowest workers
grep "worker completed" /tmp/pgclone.out | jq -r 'select(.total_time_sec > 300) | .worker_id'

# Compare transfer rates
grep "worker completed" /tmp/pgclone.out | jq -r '.transfer_rate_mbps' | sort -n

# Check file distribution
grep "worker starting" /tmp/pgclone.out | jq -r '.total_size_gb' | sort -n
```

## Expected Behavior

- **Similar total_time_sec**: Workers should complete within 2-3x of each other
- **Consistent transfer_rate_mbps**: Should be within 50% of average (network permitting)
- **Balanced initial_total_size_gb**: Should be within 20% of average
- **Low setup_time_sec**: Should be <5% of total_time_sec

## Troubleshooting Steps

1. **Check worker completion times** - identify outliers
2. **Analyze file distribution** - ensure balanced assignment
3. **Review transfer rates** - identify network bottlenecks
4. **Check error logs** - look for rsync/awk failures
5. **Compare literal vs matched data** - understand data characteristics