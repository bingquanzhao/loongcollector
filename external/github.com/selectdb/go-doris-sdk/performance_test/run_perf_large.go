package main

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/selectdb/go-doris-sdk"
)

func main() {
	fmt.Printf("🎯 ==================== SDK Performance Test (Fixed Volume) ====================\n")
	fmt.Printf("📊 Test Goal: Fixed 100 million records, test completion time and throughput at different concurrency levels\n")
	fmt.Printf("🔬 Key Metrics: Total completion time, records/sec, MB/sec\n\n")

	// Test parameters
	totalRecords := int64(100_000_000) // 100 million records
	batchSize := 50000                 // 50k records per batch, requires 2000 batches
	concurrencies := []int{1, 4, 8, 12}

	// Calculate basic information
	totalBatches := (totalRecords + int64(batchSize) - 1) / int64(batchSize)

	// Doris configuration
	config := &doris.Config{
		Endpoints:   []string{"http://10.16.10.6:8630"},
		User:        "root",
		Password:    "",
		Database:    "test",
		Table:       "orders",
		Format:      doris.DefaultCSVFormat(),
		Retry:       &doris.Retry{MaxRetryTimes: 2, BaseIntervalMs: 200, MaxTotalTimeMs: 10000},
		GroupCommit: doris.ASYNC,
		Options: map[string]string{
			"timeout": "60",
		},
	}

	client, err := doris.NewLoadClient(config)
	if err != nil {
		fmt.Printf("❌ Failed to create client: %v\n", err)
		return
	}

	// Pre-generate test data (all batches use same data to ensure test consistency)
	fmt.Printf("🔧 Pre-generating test data (%s records)...\n", formatNumber(int64(batchSize)))
	testData := generateTestData(0, 0, batchSize) // Use fixed parameters to generate standard data
	dataSize := int64(len(testData))

	// Calculate total data size based on actual data
	singleRecordSize := float64(dataSize) / float64(batchSize)                     // Actual size of single record
	totalDataSize := float64(totalRecords) * singleRecordSize / 1024 / 1024 / 1024 // Total data size (GB)

	fmt.Printf("✅ Data generation complete, single batch size: %.2f MB\n", float64(dataSize)/1024/1024)
	fmt.Printf("📏 Actual single record size: %.1f bytes\n", singleRecordSize)

	fmt.Printf("\n📋 Test Configuration:\n")
	fmt.Printf("   Total data: %s records\n", formatNumber(totalRecords))
	fmt.Printf("   Batch size: %s records/batch\n", formatNumber(int64(batchSize)))
	fmt.Printf("   Total batches: %s batches\n", formatNumber(totalBatches))
	fmt.Printf("   Concurrency levels: %v\n", concurrencies)
	fmt.Printf("   Estimated data size: %.3f GB\n", totalDataSize)

	// Store results
	results := make([]TestResult, 0, len(concurrencies))

	// Execute tests
	for _, concurrency := range concurrencies {
		fmt.Printf("🚀 ==================== Concurrency Level: %d ====================\n", concurrency)
		fmt.Printf("⏰ Start time: %s\n", time.Now().Format("15:04:05"))

		result := runFixedVolumeTest(client, concurrency, batchSize, totalRecords, testData, dataSize)
		results = append(results, result)

		printResult(result)
		fmt.Printf("⏰ Completion time: %s\n\n", time.Now().Format("15:04:05"))

		// Rest 10 seconds before next test round
		if concurrency < concurrencies[len(concurrencies)-1] {
			fmt.Printf("😴 Resting 10 seconds before next round...\n\n")
			time.Sleep(10 * time.Second)
		}
	}

	// Analyze results
	analyzeResults(results)
}

type TestResult struct {
	Concurrency      int
	BatchSize        int
	TotalRecords     int64
	TotalBytes       int64
	TotalBatches     int64
	SuccessBatches   int64
	FailedBatches    int64
	TotalDuration    time.Duration
	RecordsPerSecond float64
	MBPerSecond      float64
	BatchesPerSecond float64
	AvgBatchDuration time.Duration
	SuccessRate      float64
}

func runFixedVolumeTest(client *doris.DorisLoadClient, concurrency, batchSize int, totalRecords int64, testData string, dataSize int64) TestResult {
	var processedRecords, totalBytes, completedBatches, failedBatches int64
	var totalDuration int64

	startTime := time.Now()

	// Calculate required batches
	totalBatches := totalRecords / int64(batchSize)
	if totalRecords%int64(batchSize) != 0 {
		totalBatches++
	}

	fmt.Printf("📦 Need to process %d batches, %d records per batch\n", totalBatches, batchSize)

	// Use channel to distribute tasks
	batchChan := make(chan int64, totalBatches)
	for i := int64(0); i < totalBatches; i++ {
		batchChan <- i
	}
	close(batchChan)

	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for batchID := range batchChan {
				// Calculate actual record count for this batch
				currentBatchSize := batchSize
				var currentData string
				var currentDataSize int64

				if batchID == totalBatches-1 && totalRecords%int64(batchSize) != 0 {
					// Last batch if less than 50k records, need to generate corresponding amount
					currentBatchSize = int(totalRecords % int64(batchSize))
					currentData = generateTestData(0, 0, currentBatchSize)
					currentDataSize = int64(len(currentData))
				} else {
					// Use pre-generated standard data
					currentData = testData
					currentDataSize = dataSize
				}

				// Check if StringReader supports Seeking (only check first time)
				reader := doris.StringReader(currentData)
				if batchID == 0 && workerID == 0 {
					if _, ok := reader.(io.Seeker); ok {
						fmt.Printf("   ✅ StringReader supports Seeking, no extra buffering needed\n")
					} else {
						fmt.Printf("   ❌ StringReader doesn't support Seeking, SDK will buffer %.1fMB data!\n", float64(len(currentData))/1024/1024)
					}
				}

				// Execute load
				batchStart := time.Now()
				response, err := client.Load(reader)
				batchDuration := time.Since(batchStart)

				// Update statistics
				atomic.AddInt64(&totalBytes, currentDataSize)
				atomic.AddInt64(&totalDuration, int64(batchDuration))

				if err != nil || response == nil || response.Status != doris.SUCCESS {
					atomic.AddInt64(&failedBatches, 1)
					fmt.Printf("   ❌ Worker %d batch %d failed: %v\n", workerID, batchID, err)
				} else {
					atomic.AddInt64(&completedBatches, 1)
					atomic.AddInt64(&processedRecords, int64(currentBatchSize))
				}

				// Progress display (show every 100 batches)
				if atomic.LoadInt64(&completedBatches)%100 == 0 {
					progress := float64(atomic.LoadInt64(&completedBatches)) / float64(totalBatches) * 100
					fmt.Printf("   📈 Progress: %.1f%% (%d/%d batches completed)\n",
						progress, atomic.LoadInt64(&completedBatches), totalBatches)
				}
			}
		}(i)
	}

	wg.Wait()
	actualDuration := time.Since(startTime)

	// Build result
	result := TestResult{
		Concurrency:    concurrency,
		BatchSize:      batchSize,
		TotalRecords:   processedRecords,
		TotalBytes:     totalBytes,
		TotalBatches:   completedBatches + failedBatches,
		SuccessBatches: completedBatches,
		FailedBatches:  failedBatches,
		TotalDuration:  actualDuration,
	}

	// Calculate performance metrics
	seconds := actualDuration.Seconds()
	if seconds > 0 {
		result.RecordsPerSecond = float64(processedRecords) / seconds
		result.MBPerSecond = float64(totalBytes) / 1024 / 1024 / seconds
		result.BatchesPerSecond = float64(completedBatches) / seconds
	}

	if completedBatches > 0 {
		result.AvgBatchDuration = time.Duration(totalDuration / completedBatches)
	}

	if result.TotalBatches > 0 {
		result.SuccessRate = float64(completedBatches) / float64(result.TotalBatches) * 100
	}

	return result
}

func printResult(result TestResult) {
	fmt.Printf("📊 ==================== Test Results (Concurrency: %d) ====================\n", result.Concurrency)

	// Data volume information
	fmt.Printf("📦 Data Processing:\n")
	fmt.Printf("   📊 Processed records: %s\n", formatNumber(result.TotalRecords))
	fmt.Printf("   💾 Total data: %.3f GB\n", float64(result.TotalBytes)/1024/1024/1024)
	fmt.Printf("   📦 Successful batches: %d/%d (success rate: %.2f%%)\n",
		result.SuccessBatches, result.TotalBatches, result.SuccessRate)

	// Time information
	fmt.Printf("⏱️  Time Consumption:\n")
	fmt.Printf("   🕐 Total time: %v\n", result.TotalDuration.Round(time.Millisecond))
	fmt.Printf("   📦 Avg batch time: %v\n", result.AvgBatchDuration.Round(time.Millisecond))

	// Throughput metrics
	fmt.Printf("🚀 Throughput Metrics:\n")
	fmt.Printf("   📊 %s records/sec\n", formatNumber(int64(result.RecordsPerSecond)))
	fmt.Printf("   💿 %.2f MB/sec\n", result.MBPerSecond)
	fmt.Printf("   📦 %.1f batches/sec\n", result.BatchesPerSecond)

	if result.FailedBatches > 0 {
		fmt.Printf("⚠️  Failure Information:\n")
		fmt.Printf("   ❌ Failed batches: %d\n", result.FailedBatches)
	}
	fmt.Printf("========================================================\n")
}

func analyzeResults(results []TestResult) {
	fmt.Printf("\n🎯 ==================== Performance Comparison Analysis ====================\n")

	// Detailed comparison table
	fmt.Printf("┌────────┬──────────┬──────────┬──────────┬──────────┬──────────┬──────────┐\n")
	fmt.Printf("│ Concur │ Duration │ Data(GB) │ Rec/sec  │  MB/sec  │ Success  │ Scaling  │\n")
	fmt.Printf("├────────┼──────────┼──────────┼──────────┼──────────┼──────────┼──────────┤\n")

	var baselinePerformance float64

	for i, result := range results {
		// Calculate scaling efficiency
		var efficiency float64
		if i == 0 {
			baselinePerformance = result.RecordsPerSecond
			efficiency = 100.0 // Baseline is 100%
		} else {
			theoreticalPerformance := baselinePerformance * float64(result.Concurrency)
			efficiency = (result.RecordsPerSecond / theoreticalPerformance) * 100
		}

		fmt.Printf("│ %-6d │ %-8v │ %-8.3f │ %-8s │ %-8.2f │ %-8.1f%% │ %-8.1f%% │\n",
			result.Concurrency,
			result.TotalDuration.Round(time.Second),
			float64(result.TotalBytes)/1024/1024/1024,
			formatNumber(int64(result.RecordsPerSecond)),
			result.MBPerSecond,
			result.SuccessRate,
			efficiency)
	}
	fmt.Printf("└────────┴──────────┴──────────┴──────────┴──────────┴──────────┴──────────┘\n")

	// Performance improvement analysis
	fmt.Printf("\n📈 ==================== Performance Improvement Analysis ====================\n")
	fmt.Printf("Baseline Performance (Concurrency=1):\n")
	fmt.Printf("   📊 %s records/sec | %.2f MB/sec | Time: %v\n",
		formatNumber(int64(results[0].RecordsPerSecond)),
		results[0].MBPerSecond,
		results[0].TotalDuration.Round(time.Second))

	fmt.Printf("\nComparison to Baseline at Each Concurrency Level:\n")
	for i, result := range results {
		if i == 0 {
			continue // Skip baseline itself
		}

		recordsSpeedup := result.RecordsPerSecond / results[0].RecordsPerSecond
		mbSpeedup := result.MBPerSecond / results[0].MBPerSecond
		timeReduction := float64(results[0].TotalDuration) / float64(result.TotalDuration)

		fmt.Printf("   Concurrency %d: 🚀 %.2fx throughput | ⚡ %.2fx bandwidth | ⏱️  %.2fx time reduction\n",
			result.Concurrency, recordsSpeedup, mbSpeedup, timeReduction)
	}

	// Find best performance
	var bestResult TestResult
	var bestThroughput float64

	for _, result := range results {
		if result.RecordsPerSecond > bestThroughput && result.SuccessRate >= 99.0 {
			bestThroughput = result.RecordsPerSecond
			bestResult = result
		}
	}

	fmt.Printf("\n🏆 ==================== Best Performance Configuration ====================\n")
	if bestThroughput > 0 {
		fmt.Printf("Recommended Configuration: Concurrency %d\n", bestResult.Concurrency)
		fmt.Printf("   📊 Throughput: %s records/sec (%.2fx baseline performance)\n",
			formatNumber(int64(bestThroughput)),
			bestThroughput/results[0].RecordsPerSecond)
		fmt.Printf("   💿 Bandwidth: %.2f MB/sec\n", bestResult.MBPerSecond)
		fmt.Printf("   ⏱️  Time: %v (%.1fx faster than baseline)\n",
			bestResult.TotalDuration.Round(time.Second),
			float64(results[0].TotalDuration)/float64(bestResult.TotalDuration))
		fmt.Printf("   ✅ Success rate: %.2f%%\n", bestResult.SuccessRate)
	}

	// Performance level assessment
	fmt.Printf("\n🎖️  ==================== Performance Level Assessment ====================\n")
	maxThroughput := 0.0
	for _, result := range results {
		if result.RecordsPerSecond > maxThroughput {
			maxThroughput = result.RecordsPerSecond
		}
	}

	if maxThroughput > 500000 {
		fmt.Printf("   🏅 Performance Level: Excellent (>500K rec/sec)\n")
	} else if maxThroughput > 200000 {
		fmt.Printf("   🥈 Performance Level: Good (>200K rec/sec)\n")
	} else if maxThroughput > 100000 {
		fmt.Printf("   🥉 Performance Level: Fair (>100K rec/sec)\n")
	} else if maxThroughput > 50000 {
		fmt.Printf("   👍 Performance Level: Acceptable (>50K rec/sec)\n")
	} else {
		fmt.Printf("   ⚠️  Performance Level: Needs optimization (<50K rec/sec)\n")
	}

	// Scalability analysis
	fmt.Printf("\n📊 ==================== Scalability Analysis ====================\n")
	fmt.Printf("Ideal Scaling vs Actual Scaling:\n")
	for i, result := range results {
		if i == 0 {
			fmt.Printf("   Concurrency %d: Baseline performance\n", result.Concurrency)
			continue
		}

		theoreticalPerformance := results[0].RecordsPerSecond * float64(result.Concurrency)
		efficiency := (result.RecordsPerSecond / theoreticalPerformance) * 100

		if efficiency >= 80 {
			fmt.Printf("   Concurrency %d: 🟢 Excellent scaling (efficiency: %.1f%%)\n", result.Concurrency, efficiency)
		} else if efficiency >= 60 {
			fmt.Printf("   Concurrency %d: 🟡 Good scaling (efficiency: %.1f%%)\n", result.Concurrency, efficiency)
		} else {
			fmt.Printf("   Concurrency %d: 🔴 Limited scaling (efficiency: %.1f%%)\n", result.Concurrency, efficiency)
		}
	}

	fmt.Printf("\n💡 ==================== Production Environment Recommendations ====================\n")
	hasRecommendation := false
	for _, result := range results {
		if result.SuccessRate >= 99.5 {
			fmt.Printf("   ✅ Recommend concurrency %d: Success rate %.2f%%, Throughput %s records/sec\n",
				result.Concurrency, result.SuccessRate, formatNumber(int64(result.RecordsPerSecond)))
			hasRecommendation = true
		}
	}

	if !hasRecommendation {
		fmt.Printf("   ⚠️  All configurations <99.5%% success rate, suggest checking system configuration\n")
	}

	fmt.Printf("========================================================\n")
}

// generateTestData generates test data
func generateTestData(workerID, batchID, batchSize int) string {
	estimatedSize := batchSize * 120
	data := make([]byte, 0, estimatedSize)

	for i := 0; i < batchSize; i++ {
		orderID := fmt.Sprintf("PERF_W%d_B%d_R%d_%d", workerID, batchID, i, time.Now().UnixNano()%1000000)
		record := fmt.Sprintf("%s,Customer_%d,Product_%d,Electronics,Brand_%d,1,99.99,99.99,Shipped,2024-01-01,Region_%d\n",
			orderID, i%1000, i%100, i%50, i%10)
		data = append(data, record...)
	}
	return string(data)
}

// formatNumber formats number display
func formatNumber(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	} else if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
