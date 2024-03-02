# Log Ingester
Ingests log entries from clients, writes them to a file, and uploads them to AWS S3.

![architecture.png](https://github.com/me-heer/log_ingest/blob/main/Architecture.png)

### Running
```bash
$ git clone https://github.com/me-heer/log_ingest.git
$ cd log_ingest
$ go run main.go
# optional: go run sample_log_producer.go
```
- [sample_log_producer.go](https://github.com/me-heer/log_ingest/blob/main/sample_log_producer.go) can be used for testing to send logs to `http://localhost:8080/ingest` every 500 milliseconds.

### Endpoints

#### `/ingest`
To save logs
```
POST http://localhost:8080/ingest
```

Sample Request Body
```json
[
	{"time":1685426738,"log":"test"},
	{"time":1685426739,"log":"test"},
	{"time":1685426740,"log":"test"}
]
```

#### `/query`
To search/fetch logs between a timeframe
```http
GET http://localhost:8080/query?start={unixTimestamp}&end={unixTimestamp}&text={filterString}
```

Sample Request
```http
GET http://localhost:8080/query?start=1709356032&end=1709356032&text=test
```

Sample Response
```json
[{"time":1709356030,"log":"test2"},{"time":1709356030,"log":"test2"}]
```

#### `/list`
Used for debugging. To list all logs/objects in S3 which are uploaded by this program
```http
GET http://localhost:8080/list
```

Sample Response
```json
["mihir_joshi/2024-03-02-10-37","mihir_joshi/2024-03-02-10-38","mihir_joshi/2024-03-02-10-39"]
```
