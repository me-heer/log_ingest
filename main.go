package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/joho/godotenv"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LogEntry struct {
	Timestamp int64  `json:"time"`
	Message   string `json:"log"`
}

var (
	logChannel         = make(chan LogEntry, 100000)
	logsDirectory      = "./logs"
	accessKeyID        = os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey    = os.Getenv("AWS_SECRET_ACCESS_KEY")
	region             = os.Getenv("AWS_REGION")
	bucketName         = os.Getenv("S3_BUCKET_NAME")
	s3ObjectKeysPrefix = "mihir_joshi/"
)

/*
To handle ingestion of logs.
This handler writes logEntries to the in-memory buffer logChannel

POST http://localhost:8080/ingest

[

	{"time":1685426738,"log":"test"},
	{"time":1685426739,"log":"test"},
	{"time":1685426740,"log":"test"}

]
*/
func ingestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	// Parse the JSON log entries array
	var logEntries []LogEntry
	err = json.Unmarshal(body, &logEntries)
	if err != nil {
		http.Error(w, "Failed to parse log entries", http.StatusBadRequest)
		return
	}

	for _, logEntry := range logEntries {
		fmt.Println("Processing log entry: ", logEntry.Timestamp, logEntry.Message)
		logChannel <- logEntry
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Log entry stored successfully")
}

/*
This handler parses the start and end timestamps,
generates a list of possible S3ObjectKeys for each minute,
queries S3 for the list of files

GET http://localhost:8080/query?start=1685426738&end=1685426739&text=test
*/
func queryHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	startTimestamp := r.URL.Query().Get("start")
	endTimestamp := r.URL.Query().Get("end")
	textFilter := r.URL.Query().Get("text")

	// Parse start timestamp
	startTimeUnix, err := strconv.ParseInt(startTimestamp, 10, 64)
	startTimeUnix = startTimeUnix - 1 // To get inclusive results when filtering the log entries using .After()
	if err != nil {
		http.Error(w, "Invalid start timestamp", http.StatusBadRequest)
		return
	}
	startTime := time.Unix(startTimeUnix, 0)

	// Parse end timestamp
	endTimeUnix, err := strconv.ParseInt(endTimestamp, 10, 64)
	endTimeUnix = endTimeUnix + 1 // To get inclusive results when filtering the log entries using .Before()
	if err != nil {
		http.Error(w, "Invalid end timestamp", http.StatusBadRequest)
		return
	}
	endTime := time.Unix(endTimeUnix, 0)
	endMinute := endTime.Format("2006-01-02-15-04")

	// Generate a list of timestamps between start and end timestamps
	var timestamps []string
	for t := startTime; t.Before(endTime); t = t.Add(time.Minute) {
		timestamps = append(timestamps, t.Format("2006-01-02-15-04"))
	}
	timestamps = append(timestamps, endMinute)

	// Retrieve objects from S3 for each timestamp in the list
	var result []LogEntry
	for _, timestamp := range timestamps {
		// Get object from S3
		objectContent, err := getS3ObjectByKey(bucketName, timestamp)
		if err != nil {
			log.Printf("Error getting S3 object for timestamp %s: %v", timestamp, err)
			continue
		}

		// Unmarshal object content
		var logEntries []LogEntry
		if err := json.Unmarshal(objectContent, &logEntries); err != nil {
			log.Printf("Error unmarshalling object content for timestamp %s: %v", timestamp, err)
			continue
		}

		var filteredLogEntries []LogEntry
		for _, entry := range logEntries {
			entryTimestamp := time.Unix(entry.Timestamp, 0)
			if entryTimestamp.After(startTime) && entryTimestamp.Before(endTime) {
				filteredLogEntries = append(filteredLogEntries, entry)
			}
		}
		logEntries = filteredLogEntries

		if textFilter != "" {
			var filteredLogEntries []LogEntry
			for _, entry := range logEntries {
				if strings.Contains(entry.Message, textFilter) {
					filteredLogEntries = append(filteredLogEntries, entry)
				}
			}
			result = append(result, filteredLogEntries...)
		} else {
			result = append(result, logEntries...)
		}
	}

	// Marshal the filtered log entries and send as response
	responseData, err := json.Marshal(result)
	if err != nil {
		http.Error(w, "Error marshalling response data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseData)
}

func getS3ObjectByKey(bucketName, key string) ([]byte, error) {
	// Create a new AWS session
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretAccessKey, ""),
	})
	if err != nil {
		log.Fatalf("Error creating AWS session: %v", err)
	}

	// Create an S3 client
	svc := s3.New(sess)

	key = s3ObjectKeysPrefix + key
	resp, err := svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting object from S3: %v", err)
	}
	defer resp.Body.Close()

	// Read the object content
	objectContent, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading object content: %v", err)
	}

	return objectContent, nil
}

/*
GET http://localhost:8080/list

Returns a list of all the S3 keys created by this project
*/
func listHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Create a new AWS session
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretAccessKey, ""),
	})
	if err != nil {
		log.Fatalf("Error creating AWS session: %v", err)
		return
	}

	// Create an S3 client
	svc := s3.New(sess)

	// Initialize the list of keys
	var keys []string

	// List objects in the bucket
	err = svc.ListObjectsPages(&s3.ListObjectsInput{
		Prefix: aws.String(s3ObjectKeysPrefix),
		Bucket: aws.String(bucketName),
	}, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		// Append keys to the list
		for _, obj := range page.Contents {
			keys = append(keys, *obj.Key)
		}
		// Return true to continue listing if there are more pages
		return !lastPage
	})
	if err != nil {
		log.Fatalf("error listing bucket objects: %v", err)
		return
	}

	// Convert the list of keys to JSON
	keysJSON, err := json.Marshal(keys)
	if err != nil {
		http.Error(w, fmt.Sprintf("error marshalling keys to JSON: %v", err), http.StatusInternalServerError)
		return
	}
	// Set Content-Type header
	w.Header().Set("Content-Type", "application/json")

	// Write the JSON response
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(keysJSON)
	if err != nil {
		log.Printf("error writing response: %v", err)
	}
}

func periodicallyWriteToStorage() {
	// Create a ticker to trigger write operations every 5 seconds
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Read all log entries from the channel
			var logs []LogEntry
			for {
				select {
				case logEntry := <-logChannel:
					logs = append(logs, logEntry)
				default:
					if len(logs) > 0 {
						sort.Slice(logs, func(i, j int) bool {
							return logs[i].Timestamp < logs[j].Timestamp
						})

						currentTime := time.Now()

						currentMinuteFileName := fmt.Sprintf("%d-%02d-%02d-%02d-%02d.txt",
							currentTime.Year(),
							currentTime.Month(),
							currentTime.Day(),
							currentTime.Hour(),
							currentTime.Minute())

						fileName := filepath.Join(logsDirectory, currentMinuteFileName)

						f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
						if err != nil {
							log.Printf("Error opening log file %s: %v", fileName, err)
							continue
						}
						defer f.Close()

						// Write sorted log entries to the file
						for _, entry := range logs {
							_, err := fmt.Fprintf(f, "{\"time\":  %d, \"log\":\"%s\"}\n", entry.Timestamp, entry.Message)
							if err != nil {
								log.Printf("Error writing log to file: %v", err)
							}
						}

						// Clear the logs slice
						logs = nil
					}
					// Break the inner loop and wait for the next tick
					break
				}
			}
		}
	}
}

func periodicallyUploadToS3() {
	for {
		// List all files in logsDirectory
		files, err := os.ReadDir(logsDirectory)
		if err != nil {
			log.Printf("Error reading directory: %v", err)
			continue
		}

		// Get the current time
		currentTime := time.Now()

		// Iterate over files
		for _, file := range files {
			// Get the file modification time
			fileInfo, err := file.Info()
			if err != nil {
				log.Printf("Error reading file info: %v", err)
				continue
			}

			// Calculate the time difference
			diff := currentTime.Sub(fileInfo.ModTime()).Seconds()

			// Since we create files per minute, if the file is older than a minute, we can upload it since it will not be used again
			if diff >= 5 { // allowing for a 5-second delay in file update
				uploadToS3WithPrefix(filepath.Join(logsDirectory, file.Name()))
			}
		}

		// Sleep for some time before the next scan
		time.Sleep(1 * time.Second)
	}
}

func uploadToS3WithPrefix(fileName string) {
	// Read all lines from the file
	fileLines, err := os.ReadFile(fileName)
	if err != nil {
		log.Printf("Error reading file: %v", err)
		return
	}

	// Parse each line into a LogEntry
	var logEntries []LogEntry
	for _, line := range strings.Split(string(fileLines), "\n") {
		var entry LogEntry
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			log.Printf("Error parsing log entry: %v", err)
			continue
		}
		logEntries = append(logEntries, entry)
	}

	// Marshal the log entries into JSON format
	jsonData, err := json.Marshal(logEntries)
	if err != nil {
		log.Printf("Error marshalling log entries: %v", err)
		return
	}

	// Create a new AWS session
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretAccessKey, ""),
	})
	if err != nil {
		log.Fatalf("Error creating AWS session: %v", err)
		return
	}

	// Create an S3 client
	svc := s3.New(sess)

	logKey := s3ObjectKeysPrefix + strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	_, err = svc.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(logKey),
		Body:   bytes.NewReader(jsonData),
	})
	if err != nil {
		log.Printf("Error uploading file to S3: %v", err)
		return
	}

	log.Printf("Log entries from file %s uploaded to S3 successfully", fileName)

	err = os.Remove(fileName)
	if err != nil {
		log.Printf("Error deleting local file %s: %v", fileName, err)
	}
}

func init() {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	accessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	region = os.Getenv("AWS_REGION")
	bucketName = os.Getenv("S3_BUCKET_NAME")
}

func main() {
	go periodicallyWriteToStorage()
	go periodicallyUploadToS3()

	http.HandleFunc("/ingest", ingestHandler)
	http.HandleFunc("/query", queryHandler)
	http.HandleFunc("/list", listHandler)

	fmt.Println("Log Ingestion Started on port 8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
