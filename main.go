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

// Infrastructure Production Constants
const (
	PosAPI          = "http://localhost:8000/api/Product"
	SupabaseURL     = "https://supabase.co"
	SupabaseAPIKey  = "YOUR_SECRET_SERVICE_ROLE_JWT"
	HTTPTimeout     = 30 * time.Second
	MaxRetries      = 3
	InitialBackoff  = 2 * time.Second
)

type SupabaseTimeResponse struct {
	UpdatedAt string `json:"updated_at"`
}

type SwiftposProduct struct {
	ProductCode          string  `json:"ProductCode"`
	Description          string  `json:"Description"`
	SalesPrice           float64 `json:"SalesPrice"`
	CurrentStock         int     `json:"CurrentStock"`
	LastModifiedDateTime string  `json:"LastModifiedDateTime"`
}

type SupabasePayload struct {
	SwiftposID string    `json:"swiftpos_id"`
	Name       string    `json:"name"`
	Price      float64   `json:"price"`
	StockQty   int       `json:"stock_quantity"`
	UpdatedAt  string    `json:"updated_at"`
}

func main() {
	// Enforce strict global process execution limit
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: HTTPTimeout}

	// 1. Get high-water mark timestamp from Supabase
	var lastSyncTime string
	err := retryExecute(ctx, MaxRetries, InitialBackoff, func() error {
		reqURL := fmt.Sprintf("%s?select=updated_at&order=updated_at.desc&limit=1", SupabaseURL)
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("apikey", SupabaseAPIKey)
		req.Header.Set("Authorization", "Bearer "+SupabaseAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("supabase fetch timestamp failed with status: %d", resp.StatusCode)
		}

		var cloudData []SupabaseTimeResponse
		if err := json.NewDecoder(resp.Body).Decode(&cloudData); err != nil {
			return err
		}

		lastSyncTime = "1970-01-01T00:00:00Z"
		if len(cloudData) > 0 && cloudData[0].UpdatedAt != "" {
			lastSyncTime = cloudData[0].UpdatedAt
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "[CRITICAL ERROR] Failed to fetch sync cursor from Cloud DB: %v\n", err)
		os.Exit(1)
	}

	// 2. Fetch modified records from Swiftpos API
	var posProducts []SwiftposProduct
	err = retryExecute(ctx, MaxRetries, InitialBackoff, func() error {
		posURL := fmt.Sprintf("%s?modifiedSince=%s", PosAPI, lastSyncTime)
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
			return fmt.Errorf("swiftpos API returned unexpected status code: %d", resp.StatusCode)
		}

		return json.NewDecoder(resp.Body).Decode(&posProducts)
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "[CRITICAL ERROR] Failed to pull delta from local Swiftpos API: %v\n", err)
		os.Exit(1)
	}

	// Clean exit if no mutations detected
	if len(posProducts) == 0 {
		fmt.Println("State synchronization verified. No inventory changes found.")
		return
	}

	// 3. Map memory records to Postgres target schema
	payload := make([]SupabasePayload, 0, len(posProducts))
	for _, item := range posProducts {
		payload = append(payload, SupabasePayload{
			SwiftposID: item.ProductCode,
			Name:       item.Description,
			Price:      item.SalesPrice,
			StockQty:   item.CurrentStock,
			UpdatedAt:  item.LastModifiedDateTime,
		})
	}

	// 4. Batch Upsert to Cloud Ingress with transaction safety directives
	err = retryExecute(ctx, MaxRetries, InitialBackoff, func() error {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return err
		}

		pushReq, err := http.NewRequestWithContext(ctx, "POST", SupabaseURL, &buf)
		if err != nil {
			return err
		}
		pushReq.Header.Set("apikey", SupabaseAPIKey)
		pushReq.Header.Set("Authorization", "Bearer "+SupabaseAPIKey)
		pushReq.Header.Set("Content-Type", "application/json")
		pushReq.Header.Set("Prefer", "resolution=merge-duplicates") // Postgres idempotent upsert directive

		pushResp, err := client.Do(pushReq)
		if err != nil {
			return err
		}
		defer pushResp.Body.Close()

		if pushResp.StatusCode < 200 || pushResp.StatusCode >= 300 {
			bodyBytes, _ := io.ReadAll(pushResp.Body)
			return fmt.Errorf("supabase rejection (HTTP %d): %s", pushResp.StatusCode, string(bodyBytes))
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "[CRITICAL ERROR] Data stream failed to push to cloud infrastructure: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[%s] Success. Synchronized %d changesets safely.\n", time.Now().Format(time.RFC3339), len(payload))
}

// Resilient network execution engine wrapping retry mechanisms using standard library constructs
func retryExecute(ctx context.Context, maxRetries int, backoff time.Duration, operation func() error) error {
	for i := 0; i < maxRetries; i++ {
		if err := operation(); err == nil {
			return nil
		} else {
			fmt.Fprintf(os.Stdout, "[WARN] Action attempt %d failed: %v. Retrying...\n", i+1, err)
		}

		if i < maxRetries-1 {
			// Calculate Exponential Backoff delay duration
			sleepTime := backoff * time.Duration(math.Pow(2, float64(i)))
			select {
			case <-time.After(sleepTime):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("all database execution operations exhausted after %d attempts", maxRetries)
}

