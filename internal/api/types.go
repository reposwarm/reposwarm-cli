package api

// HealthResponse from GET /health.
type HealthResponse struct {
	Status   string `json:"status"`
	Version  string `json:"version"`
	Temporal struct {
		Connected bool   `json:"connected"`
		Namespace string `json:"namespace"`
		TaskQueue string `json:"taskQueue"`
	} `json:"temporal"`
	DynamoDB struct {
		Connected bool `json:"connected"`
	} `json:"dynamodb"`
	Worker struct {
		Connected bool `json:"connected"`
		Count     int  `json:"count"`
	} `json:"worker"`
}

// Repository from GET /repos.
type Repository struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"`
	HasDocs     bool   `json:"hasDocs"`
}

// DiscoverResult from POST /repos/discover.
type DiscoverResult struct {
	Success      bool     `json:"success"`
	Discovered   int      `json:"discovered"`
	Added        int      `json:"added"`
	Skipped      int      `json:"skipped"`
	Total        int      `json:"total"`
	Repositories []string `json:"repositories"`
}

// WorkflowExecution from GET /workflows.
type WorkflowExecution struct {
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
	Status     string `json:"status"`
	Type       string `json:"type"`
	StartTime  string `json:"startTime"`
	CloseTime  string `json:"closeTime,omitempty"`
	TaskQueue  string `json:"taskQueue,omitempty"`
}

// WorkflowsResponse from GET /workflows.
type WorkflowsResponse struct {
	Executions    []WorkflowExecution `json:"executions"`
	NextPageToken string              `json:"nextPageToken,omitempty"`
}

// WorkflowHistory from GET /workflows/:id/history.
type WorkflowHistory struct {
	Events []map[string]any `json:"events"`
}

// InvestigateRequest for POST /investigate/single.
type InvestigateRequest struct {
	RepoName  string `json:"repo_name"`
	Model     string `json:"model"`
	ChunkSize int    `json:"chunk_size"`
}

// InvestigateResponse from POST /investigate/single.
type InvestigateResponse struct {
	WorkflowID string `json:"workflowId"`
	Message    string `json:"message"`
}

// InvestigateDailyRequest for POST /investigate/daily.
type InvestigateDailyRequest struct {
	Model         string `json:"model,omitempty"`
	ChunkSize     int    `json:"chunk_size,omitempty"`
	ParallelLimit int    `json:"parallel_limit,omitempty"`
}

// WikiRepoSummary from GET /wiki.
type WikiRepoSummary struct {
	Name         string   `json:"name"`
	SectionCount int      `json:"sectionCount"`
	LastUpdated  string   `json:"lastUpdated"`
	Highlights   []string `json:"highlights"`
}

// WikiReposResponse from GET /wiki.
type WikiReposResponse struct {
	Repos []WikiRepoSummary `json:"repos"`
}

// WikiSection from GET /wiki/:repo.
type WikiSection struct {
	ID        string `json:"id"`
	StepName  string `json:"stepName"`
	Label     string `json:"label"`
	Timestamp int64  `json:"timestamp"`
	CreatedAt string `json:"createdAt"`
}

// Name returns the section identifier (prefers ID, falls back to StepName).
func (s WikiSection) Name() string {
	if s.ID != "" {
		return s.ID
	}
	return s.StepName
}

// WikiIndex from GET /wiki/:repo.
type WikiIndex struct {
	Repo     string        `json:"repo"`
	Sections []WikiSection `json:"sections"`
	HasDocs  bool          `json:"hasDocs"`
}

// WikiContent from GET /wiki/:repo/:section.
type WikiContent struct {
	Repo         string `json:"repo"`
	Section      string `json:"section"`
	Content      string `json:"content"`
	CreatedAt    string `json:"createdAt"`
	Timestamp    int64  `json:"timestamp"`
	ReferenceKey string `json:"referenceKey"`
}

// ConfigResponse from GET /config.
type ConfigResponse struct {
	DefaultModel       string `json:"defaultModel"`
	ChunkSize          int    `json:"chunkSize"`
	SleepDuration      int    `json:"sleepDuration"`
	ParallelLimit      int    `json:"parallelLimit"`
	TokenLimit         int    `json:"tokenLimit"`
	ScheduleExpression string `json:"scheduleExpression"`
}

// Prompt from GET /prompts.
type Prompt struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Template    string `json:"template"`
	Enabled     bool   `json:"enabled"`
	Order       int    `json:"order"`
	Version     int    `json:"version"`
	Context     string `json:"context,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// PromptVersion from GET /prompts/:name/versions.
type PromptVersion struct {
	Version   int    `json:"version"`
	Template  string `json:"template"`
	CreatedAt string `json:"createdAt"`
	Author    string `json:"author,omitempty"`
}

// PromptType from GET /prompts/types.
type PromptType struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
