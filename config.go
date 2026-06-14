package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	PosAPIUrl      string `json:"pos_api_url"`
	SupabaseURL    string `json:"supabase_url"`
	SupabaseAPIKey string `json:"supabase_api_key"`
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	file, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer file.Close()

	err = json.NewDecoder(file).Decode(&cfg)
	return cfg, err
}
