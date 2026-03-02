package api

// WorkerInfo represents a worker's status and health.
type WorkerInfo struct {
	Name        string   `json:"name"`
	Identity    string   `json:"identity"`
	Status      string   `json:"status"`
	TaskQueue   string   `json:"taskQueue"`
	CurrentTask string   `json:"currentTask,omitempty"`
	LastActivity string  `json:"lastActivity,omitempty"`
	EnvStatus   string   `json:"envStatus"`
	EnvErrors   []string `json:"envErrors,omitempty"`
	PID         int      `json:"pid,omitempty"`
	Uptime      string   `json:"uptime,omitempty"`
	Host        string   `json:"host,omitempty"`
	Model       string   `json:"model,omitempty"`
}

// WorkersResponse from GET /workers.
type WorkersResponse struct {
	Workers []WorkerInfo `json:"workers"`
	Total   int          `json:"total"`
	Healthy int          `json:"healthy"`
}

// ServiceInfo represents a running service.
type ServiceInfo struct {
	Name    string `json:"name"`
	PID     int    `json:"pid"`
	Status  string `json:"status"`
	Uptime  string `json:"uptime,omitempty"`
	Port    int    `json:"port,omitempty"`
	Manager string `json:"manager,omitempty"`
}
