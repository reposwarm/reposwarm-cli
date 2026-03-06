package bootstrap

import "fmt"

// Plan represents the full setup plan shown to the user before starting.
type Plan struct {
	InstallDir     string
	TemporalPort   string
	TemporalUIPort string
	APIPort        string
	UIPort         string
	DynamoDBPort   string
	DynamoDBTable  string
	APIRepoURL     string
	WorkerRepoURL  string
	UIRepoURL      string
	Region         string
	DefaultModel   string
}

// PlanFromConfig creates a Plan from the bootstrap config.
func PlanFromConfig(cfg *Config, installDir string) *Plan {
	return &Plan{
		InstallDir:     installDir,
		TemporalPort:   cfg.TemporalPort,
		TemporalUIPort: cfg.TemporalUIPort,
		APIPort:        cfg.APIPort,
		UIPort:         cfg.UIPort,
		DynamoDBPort:   "8000",
		DynamoDBTable:  cfg.DynamoDBTable,
		APIRepoURL:     cfg.APIRepoURL,
		WorkerRepoURL:  cfg.WorkerRepoURL,
		UIRepoURL:      cfg.UIRepoURL,
		Region:         cfg.Region,
		DefaultModel:   cfg.DefaultModel,
	}
}

// Steps returns a human-readable list of what will happen.
func (p *Plan) Steps() []string {
	return []string{
		fmt.Sprintf("Create install directory at %s", p.InstallDir),
		"Pull Docker images (API, Worker, UI, Temporal, PostgreSQL, DynamoDB Local)",
		"Start all services via Docker Compose",
		"Configure the CLI to use your local API",
		"Verify everything is healthy",
	}
}

// Ports returns the port mapping for display.
func (p *Plan) Ports() [][]string {
	return [][]string{
		{"Temporal gRPC", p.TemporalPort},
		{"Temporal UI", p.TemporalUIPort},
		{"API Server", p.APIPort},
		{"Web UI", p.UIPort},
		{"DynamoDB Local", p.DynamoDBPort},
		{"PostgreSQL", "5432"},
	}
}
