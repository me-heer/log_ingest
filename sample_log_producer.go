package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Log represents the structure of a log entry
type Log struct {
	Timestamp int64  `json:"time"`
	Message   string `json:"log"`
}

func main() {
	url := "http://localhost:8080/ingest"

	for {
		// Create a Log object with the current timestamp and a fixed message
		logEntry := Log{
			Timestamp: time.Now().Unix(),
			Message:   "test",
		}

		// Create an array with a single Log object
		logEntries := []Log{logEntry}

		// Convert the array to JSON
		jsonData, err := json.Marshal(logEntries)
		if err != nil {
			fmt.Println("Error marshalling JSON:", err)
			continue
		}

		// Send the HTTP POST request with the JSON data
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Println("Error sending POST request:", err)
			continue
		}
		defer resp.Body.Close()

		fmt.Println("Response Status:", resp.Status, " Log Details - Timestamp: ", logEntry.Timestamp, " Message: ", logEntry.Message)

		// Wait for one second before sending the next request
		time.Sleep(time.Second)
	}
}
