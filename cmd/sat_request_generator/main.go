package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/joho/godotenv"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	cfdiChunkDays     = 60
	metadataChunkDays = 180
	verifyTimeout     = 120 * time.Second
	pollInterval      = 5 * time.Second
)

var terminalStates = map[string]bool{
	"SENT": true, "DOWNLOADED": true, "PROCESSED": true, "TO_DOWNLOAD": true,
	"ERROR_IN_CERTS": true, "ERROR_SAT_WS_INTERNAL": true, "ERROR_SAT_WS_UNKNOWN": true,
	"TIME_LIMIT_REACHED": true, "INFORMATION_NOT_FOUND": true, "SPLITTED": true,
}

var okStates = map[string]bool{
	"SENT": true, "DOWNLOADED": true, "PROCESSED": true, "TO_DOWNLOAD": true, "SPLITTED": true,
}

type Company struct {
	bun.BaseModel `bun:"table:company"`
	ID            int64  `bun:"id"`
	Identifier    string `bun:"identifier"`
	RFC           string `bun:"rfc"`
	WorkspaceID   int64  `bun:"workspace_id"`
}

type SATQuery struct {
	Identifier   string    `bun:"identifier"`
	State        string    `bun:"state"`
	RequestType  string    `bun:"request_type"`
	DownloadType string    `bun:"download_type"`
	CreatedAt    time.Time `bun:"created_at"`
}

type SQSMessage struct {
	CompanyIdentifier string    `json:"company_identifier"`
	CompanyRFC        string    `json:"company_rfc"`
	DownloadType      string    `json:"download_type"`
	RequestType       string    `json:"request_type"`
	IsManual          bool      `json:"is_manual"`
	Start             time.Time `json:"start"`
	End               time.Time `json:"end"`
	QueryOrigin       *string   `json:"query_origin"`
	OriginSentDate    *string   `json:"origin_sent_date"`
	WID               int64     `json:"wid"`
	CID               int64     `json:"cid"`
}

func main() {
	_ = godotenv.Load(".env.local")

	dbURL := "postgresql://solcpuser:local_dev_password@localhost:5432/ezaudita_db?sslmode=disable"
	sqsEndpoint := "http://localhost:4566"
	queueURL := "http://localhost:4566/000000000000/queue_create_query"
	s3Bucket := "solucioncp-certs-local"

	// Connect to DB
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbURL)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	// AWS clients
	cfg := getAWSConfig(sqsEndpoint)
	sqsClient := sqs.NewFromConfig(cfg)
	s3Client := s3.NewFromConfig(cfg)

	ctx := context.Background()

	// List companies
	var companies []Company
	if err := db.NewSelect().Model(&companies).Order("id").Scan(ctx); err != nil {
		log.Fatal("Failed to list companies:", err)
	}

	if len(companies) == 0 {
		fmt.Println("No companies found in the database.")
		os.Exit(1)
	}

	fmt.Println("\n=== SAT WebService Request Generator ===")
	fmt.Println("Available companies:")
	for _, c := range companies {
		fmt.Printf("  [%d] %s  (%s)\n", c.ID, c.RFC, c.Identifier)
	}

	// Prompt for company
	fmt.Print("\nCompany ID or UUID: ")
	var input string
	fmt.Scanln(&input)

	var company *Company
	if cid, err := strconv.ParseInt(input, 10, 64); err == nil {
		for _, c := range companies {
			if c.ID == cid {
				company = &c
				break
			}
		}
	} else {
		for _, c := range companies {
			if c.Identifier == input {
				company = &c
				break
			}
		}
	}

	if company == nil {
		fmt.Printf("Company not found: %s\n", input)
		os.Exit(1)
	}

	fmt.Printf("\nSelected: [%d] %s (%s...)\n", company.ID, company.RFC, company.Identifier[:12])

	// Check S3 certs (optional - warning only)
	if !checkS3Certs(ctx, s3Client, s3Bucket, company.WorkspaceID, company.ID) {
		fmt.Printf("  ⚠️  WARNING: Certificates not found in S3 (ws_%d/c_%d.*)\n", company.WorkspaceID, company.ID)
		fmt.Printf("  Bucket: %s\n", s3Bucket)
		fmt.Println("  Continuing anyway (workers will fail if certs are missing)...")
	} else {
		fmt.Println("  ✅ S3 certs: OK")
	}

	// Prompt for date range
	start := promptDate("Start date")
	end := promptDate("End date  ")

	if !start.Before(end) {
		fmt.Println("Start must be before end.")
		os.Exit(1)
	}

	// Calculate chunks
	cfdiChunks := chunkDates(start, end, cfdiChunkDays)
	metaChunks := chunkDates(start, end, metadataChunkDays)
	total := (len(cfdiChunks) + len(metaChunks)) * 2

	fmt.Println("\n--- Plan ---")
	fmt.Printf("  CFDI     : %d chunks x 2 (ISSUED+RECEIVED) = %d requests  (every %dd)\n",
		len(cfdiChunks), len(cfdiChunks)*2, cfdiChunkDays)
	fmt.Printf("  METADATA : %d chunks x 2 (ISSUED+RECEIVED) = %d requests  (every %dd)\n",
		len(metaChunks), len(metaChunks)*2, metadataChunkDays)
	fmt.Printf("  Total    : %d SQS messages -> queue_create_query\n\n", total)

	for i, chunk := range cfdiChunks {
		fmt.Printf("  CFDI     %3d/%d  %s -> %s\n", i+1, len(cfdiChunks),
			chunk.start.Format("2006-01-02"), chunk.end.Format("2006-01-02"))
	}
	for i, chunk := range metaChunks {
		fmt.Printf("  METADATA %3d/%d  %s -> %s\n", i+1, len(metaChunks),
			chunk.start.Format("2006-01-02"), chunk.end.Format("2006-01-02"))
	}

	fmt.Printf("\nSend %d messages? (yes/no): ", total)
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) != "yes" {
		fmt.Println("Cancelled.")
		os.Exit(0)
	}

	// Send messages
	sent := 0
	for _, chunk := range cfdiChunks {
		for _, dt := range []string{"ISSUED", "RECEIVED"} {
			sendMessage(ctx, sqsClient, queueURL, company, "CFDI", dt, chunk.start, chunk.end)
			sent++
		}
	}
	for _, chunk := range metaChunks {
		for _, dt := range []string{"ISSUED", "RECEIVED"} {
			sendMessage(ctx, sqsClient, queueURL, company, "METADATA", dt, chunk.start, chunk.end)
			sent++
		}
	}

	fmt.Printf("\n%d messages sent. Waiting for worker to process...\n\n", sent)

	// Verify results
	results := verifyResults(ctx, db, company.Identifier, total)

	okCount := 0
	errCount := 0
	for _, r := range results {
		if okStates[r.State] {
			okCount++
		} else {
			errCount++
		}
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  RESULTS: %d OK  /  %d ERRORS  /  %d expected\n", okCount, errCount, total)
	fmt.Println(strings.Repeat("=", 60))

	for _, r := range results {
		status := "OK"
		if !okStates[r.State] {
			status = "!!"
		}
		fmt.Printf("  [%s] %s... %8s %8s  -> %s\n",
			status, r.Identifier[:12], r.RequestType, r.DownloadType, r.State)
	}

	if errCount > 0 {
		fmt.Printf("\n  WARNING: %d queries failed. Check worker logs.\n", errCount)
		os.Exit(1)
	} else {
		fmt.Printf("\n  All %d queries created successfully.\n", okCount)
	}
}

type dateChunk struct {
	start time.Time
	end   time.Time
}

func chunkDates(start, end time.Time, days int) []dateChunk {
	var chunks []dateChunk
	cursor := start
	for cursor.Before(end) {
		chunkEnd := cursor.AddDate(0, 0, days)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		chunks = append(chunks, dateChunk{start: cursor, end: chunkEnd})
		cursor = chunkEnd
	}
	return chunks
}

func promptDate(label string) time.Time {
	for {
		fmt.Printf("  %s (YYYY-MM-DD): ", label)
		var input string
		fmt.Scanln(&input)
		t, err := time.Parse("2006-01-02", input)
		if err == nil {
			return t
		}
		fmt.Println("    Invalid format. Use YYYY-MM-DD.")
	}
}

func checkS3Certs(ctx context.Context, s3Client *s3.Client, bucket string, wid, cid int64) bool {
	for _, ext := range []string{"cer", "key", "txt"} {
		key := fmt.Sprintf("ws_%d/c_%d.%s", wid, cid, ext)
		_, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return false
		}
	}
	return true
}

func sendMessage(ctx context.Context, sqsClient *sqs.Client, queueURL string, company *Company,
	requestType, downloadType string, start, end time.Time) {

	msg := SQSMessage{
		CompanyIdentifier: company.Identifier,
		CompanyRFC:        company.RFC,
		DownloadType:      downloadType,
		RequestType:       requestType,
		IsManual:          true,
		Start:             start,
		End:               end,
		WID:               company.WorkspaceID,
		CID:               company.ID,
	}

	body, _ := json.Marshal(msg)
	_, err := sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		log.Printf("Failed to send message: %v", err)
	}
}

func verifyResults(ctx context.Context, db *bun.DB, tenantSchema string, expected int) []SATQuery {
	startTime := time.Now()
	lastCount := 0

	for time.Since(startTime) < verifyTimeout {
		var results []SATQuery
		_, err := db.NewRaw(
			fmt.Sprintf(`SELECT identifier, state, request_type, download_type, created_at 
				FROM "%s".sat_query ORDER BY created_at DESC LIMIT ?`, tenantSchema),
			expected+10,
		).Exec(ctx, &results)

		if err != nil {
			log.Printf("Query error: %v", err)
			time.Sleep(pollInterval)
			continue
		}

		if len(results) > expected {
			results = results[:expected]
		}

		terminalCount := 0
		for _, r := range results {
			if terminalStates[r.State] {
				terminalCount++
			}
		}

		if terminalCount != lastCount {
			lastCount = terminalCount
			elapsed := int(time.Since(startTime).Seconds())
			fmt.Printf("  [%ds] %d/%d queries in terminal state...\n", elapsed, terminalCount, expected)
		}

		if terminalCount >= expected {
			return results
		}

		time.Sleep(pollInterval)
	}

	// Timeout - return what we have
	var results []SATQuery
	_, _ = db.NewRaw(
		fmt.Sprintf(`SELECT identifier, state, request_type, download_type, created_at 
			FROM "%s".sat_query ORDER BY created_at DESC LIMIT ?`, tenantSchema),
		expected,
	).Exec(ctx, &results)
	return results
}

func getAWSConfig(endpoint string) aws.Config {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: endpoint}, nil
			})),
	)
	if err != nil {
		log.Fatal(err)
	}
	return cfg
}
