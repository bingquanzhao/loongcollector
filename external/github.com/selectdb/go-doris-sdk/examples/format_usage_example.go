package examples

import (
	"fmt"

	"github.com/selectdb/go-doris-sdk"
)

// FormatUsageExample demonstrates how to use Format interface
func FormatUsageExample() {
	fmt.Println("=== Format Interface Usage Example ===")

	// Method 1: Direct JSONFormat construction (recommended)
	jsonConfig := &doris.Config{
		Endpoints: []string{"http://localhost:8630"},
		User:      "root",
		Password:  "password",
		Database:  "example_db",
		Table:     "example_table",
		// Direct JSONFormat struct construction
		Format: &doris.JSONFormat{
			Type: doris.JSONObjectLine, // or doris.JSONArray
		},
		Retry: &doris.Retry{
			MaxRetryTimes:  3,
			BaseIntervalMs: 1000,
			MaxTotalTimeMs: 60000, // Add total time limit
		},
		GroupCommit: doris.ASYNC,
	}

	// Method 2: Direct CSVFormat construction (recommended)
	csvConfig := &doris.Config{
		Endpoints: []string{"http://localhost:8630"},
		User:      "root",
		Password:  "password",
		Database:  "example_db",
		Table:     "example_table",
		// Direct CSVFormat struct construction
		Format: &doris.CSVFormat{
			ColumnSeparator: ",",
			LineDelimiter:   "\n",
		},
		Retry: &doris.Retry{
			MaxRetryTimes:  5,
			BaseIntervalMs: 2000,
			MaxTotalTimeMs: 60000, // Add total time limit
		},
		GroupCommit: doris.SYNC,
	}

	// Method 3: Custom format configuration
	customConfig := &doris.Config{
		Endpoints: []string{"http://localhost:8630"},
		User:      "root",
		Password:  "password",
		Database:  "example_db",
		Table:     "example_table",
		// Custom CSV separator
		Format: &doris.CSVFormat{
			ColumnSeparator: "|",   // Pipe separator
			LineDelimiter:   "\\n", // Custom line delimiter
		},
		Retry:       doris.DefaultRetry(),
		GroupCommit: doris.OFF,
	}

	// Demonstrate Format interface usage
	configs := []*doris.Config{jsonConfig, csvConfig, customConfig}
	configNames := []string{"JSON Config", "CSV Config", "Custom CSV Config"}

	for i, config := range configs {
		fmt.Printf("\n--- %s ---\n", configNames[i])
		fmt.Printf("Format Type: %s\n", config.Format.GetFormatType())
		fmt.Printf("Format Options: %v\n", config.Format.GetOptions())

		// Validate configuration
		if err := config.ValidateInternal(); err != nil {
			fmt.Printf("Validation Error: %v\n", err)
			continue
		}

		// Create client
		client, err := doris.NewLoadClient(config)
		if err != nil {
			fmt.Printf("Client Creation Error: %v\n", err)
			continue
		}

		fmt.Printf("Client created successfully for %s\n", config.Format.GetFormatType())

		// Simulate data loading
		var sampleData string
		if config.Format.GetFormatType() == "json" {
			sampleData = `{"id": 1, "name": "Alice"}
{"id": 2, "name": "Bob"}`
		} else {
			sampleData = `1,Alice
2,Bob`
		}

		fmt.Printf("Sample data for %s format:\n%s\n", config.Format.GetFormatType(), sampleData)

		// Note: This is just a demonstration, actual use requires a real Doris server
		_ = client
		_ = sampleData
	}

	fmt.Println("\n=== Example Complete ===")
}
