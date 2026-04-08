package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Database
	DBHost     string
	DBHostRO   string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string

	// AWS / LocalStack
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSEndpointURL     string
	LocalInfra         bool
	RegionName         string

	// Cloud / Azure (Phase 1E)
	// CloudProvider: "aws" (default, S3 + SQS) or "azure" (Blob + Service Bus publish when
	// AZURE_SERVICEBUS_CONNECTION_STRING is set; else Storage Queues for Azurite-only).
	CloudProvider                   string
	AzureStorageConnectionString    string
	AzureServiceBusConnectionString string

	// Self-managed auth (azure mode): HS256 signing key for JWTs.
	SelfAuthSigningKey string

	// S3 Buckets
	S3AccessKey   string
	S3SecretKey   string
	S3Certs       string
	S3Attachments string
	S3Export      string
	S3ADD         string
	// ADDS3ExpirationDelta is presign TTL for S3_ADD (COI + ADD); matches Python timedelta(days=int(os.environ.get("ADD_S3_EXPIRATION_DELTA", 7))).
	ADDS3ExpirationDelta  time.Duration
	S3FilesAttach         string
	S3UUIDsCompareScraper string

	// SQS Queues
	SQSProcessPackageMetadata string
	SQSProcessPackageXML      string
	SQSCompleteCFDIs          string
	SQSVerifyQuery            string
	SQSSendQueryMetadata      string
	SQSDownloadQuery          string
	SQSCreateQuery            string
	SQSUpdaterQuery           string
	SQSExport                 string
	SQSMassiveExport          string
	SQSNotifications          string
	SQSScrapOrchestrator      string
	SQSScrapDelayer           string
	SQSScrapResults           string
	SQSSATScrapPDF            string
	SQSPastoConfigWorker      string
	SQSPastoGetCompanies      string
	SQSADDProcessMetadata     string
	SQSADDDataSync            string
	SQSResetADDLicenseKey     string
	SQSADDMetadataRequest     string
	SQSCOIDataSync            string
	SQSCOIMetadataUploaded    string

	// Cognito
	CognitoUserPoolID   string
	CognitoClientID     string
	CognitoClientSecret string
	CognitoURL          string
	CognitoRedirectURI  string

	// Logging
	LogLevel   string
	DBLogLevel string
	DevMode    bool

	// Frontend
	FrontendBaseURL string
	SelfEndpoint    string

	// External Service Flags
	NotifyOdoo   bool
	NotifyStripe bool
	MockOdoo     bool
	MockStripe   bool

	// Pasto
	PastoURL             string
	PastoOCPKey          string
	PastoEmail           string
	PastoPassword        string
	PastoResetLicenseURL string
	PastoSubscriptionID  string
	PastoDashboardID     string
	PastoRequestTimeout  int
	PastoMaxRetries      int

	// Stripe
	StripeSecretKey                string
	StripeSetProductSecretKey      string
	StripeCoupon                   string
	StripeDefaultItems             []interface{}
	StripeDefaultTaxRates          []interface{}
	StripeDaysUntilDue             int
	StripeWebhookPaidAlert         string
	StripeCancelAtDelta            time.Duration
	StripeDefaultProrationBehavior string

	// Odoo
	OdooURL      string
	OdooDB       string
	OdooUser     string
	OdooPassword string
	OdooPort     int

	// SES
	SESMail string

	// Application
	MaxManualSyncPerDay      int
	DefaultLicenseLifetime   time.Duration
	ScrapManualStartDate     time.Time
	DaysToKeepSATQueryTable  int
	DevFIELAndCSDPassphrase  []byte
	StatisticsInfoTimeDelta  time.Duration
	StatisticsInfoPeriodSecs int
	DBClusterIdentifier      string
	MaxFileSizeKB            int
	MaxSameCompanyInTrials   int

	// ISR
	ISRDefaultPercentage float64
	ISRPercentageList    []float64

	// Special RFCs (exempted from freemium duplicate check)
	SpecialRFCs map[string]bool

	// Admin create default license (applied on admin_create flow)
	AdminCreateDefaultLicense map[string]interface{}

	// Feature Flags
	IsSiigo         bool
	BlockAppAccess  bool
	BlockAppMessage string

	// Webhook paths
	ADDConfigWebhook    string
	ADDCompaniesWebhook string
	ADDMetadataWebhook  string
	ADDXMLWebhook       string
	ADDCancelWebhook    string

	// Products (VITE)
	ProductTrial      string
	ProductADD        string
	ProductHighVolume string
	ProductExtraUsers string
	ProductDisplays   string

	// Siigo Marketing
	SiigoFreeTrialBaseURL string
	SiigoFreeTrialTimeout int

	// Admin
	AdminEmails []string
}

func (c *Config) ManualRequestStartDelta() time.Duration {
	return 72 * time.Hour
}

func Load() (*Config, error) {
	loadDotEnv()

	dbPort, _ := strconv.Atoi(envOr("DB_PORT", "5432"))
	dbHost := mustEnv("DB_HOST")

	localInfra := envBool("LOCAL_INFRA")

	cognitoClientSecret := os.Getenv("COGNITO_CLIENT_SECRET")
	if cognitoClientSecret == "N/A" {
		cognitoClientSecret = ""
	}

	isSiigo := envBool("IS_SIIGO")
	if cognitoClientSecret != "" {
		isSiigo = true
	}

	var stripeItems []interface{}
	_ = json.Unmarshal([]byte(envOr("STRIPE_DEFAULT_ITEMS", "[]")), &stripeItems)

	var stripeTaxRates []interface{}
	_ = json.Unmarshal([]byte(envOr("STRIPE_DEFAULT_TAX_RATES", "[]")), &stripeTaxRates)

	scrapStart, _ := time.Parse("2006-01-02", envOr("SCRAP_MANUAL_STARTDATE", "2025-01-01"))

	cfg := &Config{
		DBHost:     dbHost,
		DBHostRO:   envOr("DB_HOST_RO", dbHost),
		DBPort:     dbPort,
		DBName:     mustEnv("DB_NAME"),
		DBUser:     mustEnv("DB_USER"),
		DBPassword: mustEnv("DB_PASSWORD"),

		AWSAccessKeyID:     envOr("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey: envOr("AWS_SECRET_ACCESS_KEY", ""),
		AWSEndpointURL:     os.Getenv("AWS_ENDPOINT_URL"),
		LocalInfra:         localInfra,
		RegionName:         envOr("REGION_NAME", "us-east-1"),

		CloudProvider:                strings.ToLower(strings.TrimSpace(envOr("CLOUD_PROVIDER", "aws"))),
		AzureStorageConnectionString: os.Getenv("AZURE_STORAGE_CONNECTION_STRING"),
		// Service Bus namespace connection string (Send). Required for Azure SAT/event publishing
		// when queues are azurerm_servicebus_queue (not Storage Queues).
		AzureServiceBusConnectionString: strings.TrimSpace(os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")),
		SelfAuthSigningKey:              envOr("SELFAUTH_SIGNING_KEY", "change-me-in-production-use-a-64-char-hex-key-from-key-vault!!"),

		S3AccessKey:           mustEnv("S3_ACCESS_KEY"),
		S3SecretKey:           mustEnv("S3_SECRET_KEY"),
		S3Certs:               mustEnv("S3_CERTS"),
		S3Attachments:         mustEnv("S3_ATTACHMENTS"),
		S3Export:              mustEnv("S3_EXPORT"),
		S3ADD:                 mustEnv("S3_ADD"),
		ADDS3ExpirationDelta:  time.Duration(envInt("ADD_S3_EXPIRATION_DELTA", 7)) * 24 * time.Hour,
		S3FilesAttach:         mustEnv("S3_FILESATTACH"),
		S3UUIDsCompareScraper: mustEnv("S3_UUIDS_COMPARE_SCRAPER"),

		SQSProcessPackageMetadata: mustEnv("SQS_PROCESS_PACKAGE_METADATA"),
		SQSProcessPackageXML:      mustEnv("SQS_PROCESS_PACKAGE_XML"),
		SQSCompleteCFDIs:          mustEnv("SQS_COMPLETE_CFDIS"),
		SQSVerifyQuery:            mustEnv("SQS_VERIFY_QUERY"),
		SQSSendQueryMetadata:      mustEnv("SQS_SEND_QUERY_METADATA"),
		SQSDownloadQuery:          mustEnv("SQS_DOWNLOAD_QUERY"),
		SQSCreateQuery:            mustEnv("SQS_CREATE_QUERY"),
		SQSUpdaterQuery:           mustEnv("SQS_UPDATER_QUERY"),
		SQSExport:                 mustEnv("SQS_EXPORT"),
		SQSMassiveExport:          mustEnv("SQS_MASSIVE_EXPORT"),
		SQSNotifications:          mustEnv("SQS_NOTIFICATIONS"),
		SQSScrapOrchestrator:      mustEnv("SQS_SCRAP_ORCHESTRATOR"),
		SQSScrapDelayer:           mustEnv("SQS_SCRAP_DELAYER"),
		SQSScrapResults:           mustEnv("SQS_SCRAP_RESULTS"),
		SQSSATScrapPDF:            mustEnv("SQS_SAT_SCRAP_PDF"),
		SQSPastoConfigWorker:      mustEnv("SQS_PASTO_CONFIG_WORKER"),
		SQSPastoGetCompanies:      mustEnv("SQS_PASTO_GET_COMPANIES"),
		SQSADDProcessMetadata:     mustEnv("SQS_PASTO_PROCESS_METADATA"),
		SQSADDDataSync:            mustEnv("SQS_PASTO_FULL_SYNC"),
		SQSResetADDLicenseKey:     mustEnv("SQS_RESET_ADD_LICENSE_KEY"),
		SQSADDMetadataRequest:     mustEnv("SQS_ADD_SYNC_METADATA"),
		SQSCOIDataSync:            envOr("SQS_COI_DATA_SYNC", "coi_data_sync"),
		SQSCOIMetadataUploaded:    envOr("SQS_COI_METADATA_UPLOADED", "coi_metadata_uploaded"),

		CognitoUserPoolID:   os.Getenv("COGNITO_USER_POOL_ID"),
		CognitoClientID:     mustEnv("COGNITO_CLIENT_ID"),
		CognitoClientSecret: cognitoClientSecret,
		CognitoURL:          mustEnv("COGNITO_URL"),
		CognitoRedirectURI:  envOr("COGNITO_REDIRECT_URI", "http://localhost:5173/callback"),

		LogLevel:   envOr("LOG_LEVEL", "DEBUG"),
		DBLogLevel: envOr("DB_LOG_LEVEL", "WARNING"),
		DevMode:    envBool("DEV_MODE"),

		FrontendBaseURL: envOr("FRONTEND_BASE_URL", "http://localhost:5173"),
		SelfEndpoint:    os.Getenv("VITE_REACT_APP_BASE_URL"),

		NotifyOdoo:   envBool("NOTIFY_ODOO"),
		NotifyStripe: envBool("NOTIFY_STRIPE"),
		MockOdoo:     envBool("MOCK_ODOO"),
		MockStripe:   envBool("MOCK_STRIPE"),

		PastoURL:             mustEnv("PASTO_URL"),
		PastoOCPKey:          mustEnv("PASTO_OCP_KEY"),
		PastoEmail:           mustEnv("PASTO_EMAIL"),
		PastoPassword:        mustEnv("PASTO_PASSWORD"),
		PastoResetLicenseURL: mustEnv("PASTO_RESET_LICENSE_URL"),
		PastoSubscriptionID:  mustEnv("PASTO_SUBSCRIPTION_ID"),
		PastoDashboardID:     mustEnv("PASTO_DASHBOARD_ID"),
		PastoRequestTimeout:  envInt("PASTO_REQUEST_TIMEOUT", 50),
		PastoMaxRetries:      envInt("PASTO_MAX_RETRIES", 1),

		StripeSecretKey:                os.Getenv("STRIPE_SECRET_KEY"),
		StripeSetProductSecretKey:      envOr("STRIPE_SET_PRODUCT_SECRET_KEY", ""),
		StripeCoupon:                   envOr("STRIPE_COUPON", ""),
		StripeDefaultItems:             stripeItems,
		StripeDefaultTaxRates:          stripeTaxRates,
		StripeDaysUntilDue:             envInt("STRIPE_DAYS_UNTIL_DUE", 3),
		StripeWebhookPaidAlert:         envOr("STRIPE_WEBHOOK_PAID_ALERT", ""),
		StripeCancelAtDelta:            15 * 24 * time.Hour,
		StripeDefaultProrationBehavior: "always_invoice",

		OdooURL:      os.Getenv("ODOO_URL"),
		OdooDB:       os.Getenv("ODOO_DB"),
		OdooUser:     os.Getenv("ODOO_USER"),
		OdooPassword: os.Getenv("ODOO_PASSWORD"),
		OdooPort:     envInt("ODOO_PORT", 443),

		SESMail:             mustEnv("SES_MAIL"),
		DBClusterIdentifier: mustEnv("DB_CLUSTER_IDENTIFIER"),

		MaxManualSyncPerDay:      envInt("MAX_MANUAL_SYNC_PER_DAY", 5),
		DefaultLicenseLifetime:   time.Duration(envInt("DEFAULT_LICENSE_LIFETIME", 10)) * 24 * time.Hour,
		ScrapManualStartDate:     scrapStart,
		DaysToKeepSATQueryTable:  envInt("DAYS_TO_KEEP_SAT_QUERY_TABLE", 7),
		DevFIELAndCSDPassphrase:  []byte(envOr("DEV_FIEL_AND_CSD_PASSPHRASE", "")),
		StatisticsInfoTimeDelta:  time.Duration(envInt("STATISTICS_INFO_TIME_DELTA", 10)) * time.Minute,
		StatisticsInfoPeriodSecs: envInt("STATISTICS_INFO_PERIOD_SECONDS", 60),
		MaxFileSizeKB:            envInt("MAX_FILE_SIZE_KB", 500),
		MaxSameCompanyInTrials:   envInt("MAX_SAME_COMPANY_IN_TRIALS", 2),

		ISRDefaultPercentage: 0.47,
		ISRPercentageList:    []float64{0.47, 0.53},
		SpecialRFCs: map[string]bool{
			"PGD1009214W0": true,
			"CPL151127NR7": true,
		},
		AdminCreateDefaultLicense: map[string]interface{}{
			"id":         1,
			"date_start": "2025-07-08",
			"date_end":   "2035-07-08",
			"details": map[string]interface{}{
				"max_emails_enroll":     "unlimited",
				"max_companies":         "unlimited",
				"exceed_metadata_limit": false,
				"add_enabled":           false,
				"products": []map[string]interface{}{
					{"identifier": "prod_MZAVa4wGwDTZJ9", "quantity": 1},
				},
			},
			"stripe_status": "active",
		},

		IsSiigo:         isSiigo,
		BlockAppAccess:  envBool("BLOCK_APP_ACCESS"),
		BlockAppMessage: envOr("BLOCK_APP_MESSAGE", "Estamos trabajando para mejorar el sitio. Intenta acceder en un momento más..."),

		ADDConfigWebhook:    "Pasto/Config",
		ADDCompaniesWebhook: "Pasto/Company",
		ADDMetadataWebhook:  "Pasto/Metadata",
		ADDXMLWebhook:       "Pasto/XML",
		ADDCancelWebhook:    "Pasto/Cancel",

		ProductTrial:      os.Getenv("VITE_REACT_APP_PRODUCT_TRIAL"),
		ProductADD:        os.Getenv("VITE_REACT_APP_PRODUCT_ADD"),
		ProductHighVolume: os.Getenv("VITE_REACT_APP_PRODUCT_HIGHVOLUME"),
		ProductExtraUsers: os.Getenv("VITE_REACT_APP_PRODUCT_EXTRAUSERS"),
		ProductDisplays:   os.Getenv("VITE_REACT_APP_PRODUCT_DISPLAYS"),

		SiigoFreeTrialBaseURL: envOr("SIIGO_FREETRIAL_BASE_URL", "https://siigocrm.siigo.com/SiigoFreeTrial"),
		SiigoFreeTrialTimeout: envInt("SIIGO_FREETRIAL_TIMEOUT", 50),

		AdminEmails: parseAdminEmails(envOr("ADMIN_EMAILS", "admin@sg.com,main@test.com")),
	}

	return cfg, nil
}

func (c *Config) DBDSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		url.QueryEscape(c.DBUser), url.QueryEscape(c.DBPassword),
		c.DBHost, c.DBPort, c.DBName,
	)
}

func (c *Config) DBDSNReadOnly() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		url.QueryEscape(c.DBUser), url.QueryEscape(c.DBPassword),
		c.DBHostRO, c.DBPort, c.DBName,
	)
}

// loadDotEnv reads a .env file and sets environment variables that aren't already set.
func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "WARN: required env var %s is empty\n", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	v := os.Getenv(key)
	n, _ := strconv.Atoi(v)
	return n != 0
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func parseAdminEmails(raw string) []string {
	var emails []string
	for _, e := range strings.Split(raw, ",") {
		e = strings.TrimSpace(e)
		if e != "" {
			emails = append(emails, e)
		}
	}
	return emails
}
