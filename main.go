package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"
)

const (
	ConfigPath     = "C:\\Program Files\\SwiftposSync\\config.json"
	LogFilePath    = "C:\\Program Files\\SwiftposSync\\sync_agent.log"
	MaxLogSize     = 10 * 1024 * 1024 // 10MB file limitation cap
	HTTPTimeout    = 30 * time.Second
	MaxRetries     = 3
	InitialBackoff  = 2 * time.Second
	BatchChunkSize = 500
)

type SupabaseTimeResponse struct {
	UpdatedAt string `json:"updated_at"`
}

// SwiftposProduct captures modern extended text fields from the on-prem API contract
type SwiftposProduct struct {
	ProductCode          string  `json:"ProductCode"`
	Description          string  `json:"Description"` // Receipt level short string
	WebNotes             string  `json:"WebNotes"`    // Extended description block text
	SalesPrice           float64 `json:"SalesPrice"`
	CurrentStock         int     `json:"CurrentStock"`
	LastModifiedDateTime string  `json:"LastModifiedDateTime"`
	Active               bool    `json:"Active"`
}

// SupabasePayload mirrors our updated relational table design constraints
type SupabasePayload struct {
	SwiftposID  string  `json:"swiftpos_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"` // Pushed as full source-of-truth text
	Price       float64 `json:"price"`
	StockQty    int     `json:"stock_quantity"`
	UpdatedAt   string  `json:"updated_at"`
	IsVisible   bool    `json:"is_visible"`
	// Note: 'image_url' is left missing from this payload to preserve cloud imagery bindings
}

func main() {
	logFile, err := setupLogger(LogFilePath, MaxLogSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Logger deployment fault: %v\n", err)
		os.Exit(1)
	}
	if logFile != nil {
		defer logFile.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := loadConfig(ConfigPath)
	if err != nil {
		logEvent("CRITICAL", fmt.Sprintf("Failed to load secure configuration parameters: %v", err))
		os.Exit(1)
	}

	client := &http.Client{Timeout: HTTPTimeout}

	// 1. Fetch high-water mark timestamp from Supabase
	var lastSyncTime string
	err = retryExecute(ctx, MaxRetries, InitialBackoff, func() error {
		reqURL := fmt.Sprintf("%s?select=updated_at&order=updated_at.desc&limit=1", cfg.SupabaseURL)
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("apikey", cfg.SupabaseAPIKey)
		req.Header.Set("Authorization", "Bearer "+cfg.SupabaseAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("supabase timestamp server rejected block with status: %d", resp.StatusCode)
		}

		var cloudData []SupabaseTimeResponse
		if err := json.NewDecoder(resp.Body).Decode(&cloudData); err != nil {
			return err
		}

		lastSyncTime = "1970-01-01T00:00:00Z"
		if len(cloudData) > 0 && cloudData.UpdatedAt != "" {
			t, parseErr := time.Parse(time.RFC3339, cloudData.UpdatedAt)
			if parseErr == nil {
				lastSyncTime = t.UTC().Format(time.RFC3339)
			}
		}
		return nil
	})

	if err != nil {
		logEvent("CRITICAL", fmt.Sprintf("Exhausted database retry limits matching cloud timeline: %v", err))
		os.Exit(1)
	}

	// 2. Fetch changesets from local Swiftpos API
	var posProducts []SwiftposProduct
	err = retryExecute(ctx, MaxRetries, InitialBackoff, func() error {
		posURL := fmt.Sprintf("%s?modifiedSince=%s", cfg.PosAPIUrl, lastSyncTime)
		req, err := http.NewRequestWithContext(ctx, "GET", posURL, nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("local swiftpos worker instance execution fault: %d", resp.StatusCode)
		}

		return json.NewDecoder(resp.Body).Decode(&posProducts)
	})

	if err != nil {
		logEvent("CRITICAL", fmt.Sprintf("Exhausted retries mapping inbound POS records: %v", err))
		os.Exit(1)
	}

	if len(posProducts) == 0 {
		logEvent("INFO", "State alignment verified. Zero network mutations found.")
		return
	}

	// 3. Process and Clean Data Defensively (Clamping logic)
	payload := make([]SupabasePayload, 0, len(posProducts))
	for _, item := range posProducts {
		parsedTime, parseErr := time.Parse(time.RFC3339, item.LastModifiedDateTime)
		finalTimeStr := item.LastModifiedDateTime
		if parseErr == nil {
			finalTimeStr = parsedTime.UTC().Format(time.RFC3339)
		}

		sanitizedPrice := math.Max(0.0, item.SalesPrice)
		sanitizedStock := item.CurrentStock
		if sanitizedStock < 0 {
			sanitizedStock = 0 
		}

		payload = append(payload, SupabasePayload{
			SwiftposID:  item.ProductCode,
			Name:        item.Description,
			Description: item.WebNotes, // Maps extended POS text blocks out to database descriptions
			Price:       sanitizedPrice,
			StockQty:    sanitizedStock,
			UpdatedAt:   finalTimeStr,
			IsVisible:   item.Active,
		})
	}

	// 4. Batch Chunking upload pipeline
	for i := 0; i < len(payload); i += BatchChunkSize {
		end := i + BatchChunkSize
		if end > len(payload) {
			end = len(payload)
		}
		chunk := payload[i:end]

		err = retryExecute(ctx, MaxRetries, InitialBackoff, func() error {
			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(chunk); err != nil {
				return err
			}

			pushReq, err := http.NewRequestWithContext(ctx, "POST", cfg.SupabaseURL, &buf)
			if err != nil {
				return err
			}
			pushReq.Header.Set("apikey", cfg.SupabaseAPIKey)
			pushReq.Header.Set("Authorization", "Bearer "+cfg.SupabaseAPIKey)
			pushReq.Header.Set("Content-Type", "application/json")
			pushReq.Header.Set("Prefer", "resolution=merge-duplicates") // Triggers Postgres ON CONFLICT DO UPDATE

			pushResp, err := client.Do(pushReq)
			if err != nil {
				return err
			}
			defer pushResp.Body.Close()

			if pushResp.StatusCode < 200 || pushResp.StatusCode >= 300 {
				bodyBytes, _ := io.ReadAll(pushResp.Body)
				return fmt.Errorf("cloud transaction pipeline validation fault (HTTP %d): %s", pushResp.StatusCode, string(bodyBytes))
			}
			return nil
		})

		if err != nil {
			logEvent("CRITICAL", fmt.Sprintf("Pipeline batch processing crash at offset chunk %d: %v", i, err))
			os.Exit(1)
		}
	}

	logEvent("INFO", fmt.Sprintf("Successfully synchronized %d active items and content descriptors.", len(payload)))
}

func retryExecute(ctx context.Context, maxRetries int, backoff time.Duration, operation func() error) error {
	for i := 0; i < maxRetries; i++ {
		if err := operation(); err == nil {
			return nil
		}
		if i < maxRetries-1 {
			sleepTime := backoff * time.Duration(math.Pow(2, float64(i)))
			select {
			case <-time.After(sleepTime):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("execution operations exhausted after %d sequential checks", maxRetries)
}
