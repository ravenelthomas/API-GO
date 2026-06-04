package models

import "time"

type Server struct {
	ID         uint   `json:"id" gorm:"primaryKey"`
	Name       string `json:"name"`
	DockerHost string `json:"docker_host"`
	IsLocal    bool   `json:"is_local"`
	CreatedAt  time.Time
}

type Project struct {
	ID          uint             `json:"id" gorm:"primaryKey"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Services    []ServiceSpec    `json:"services,omitempty"`
	Networks    []ProjectNetwork `json:"networks,omitempty"`
	Secrets     []ProjectSecret  `json:"secrets,omitempty"`
	Labels      []ProjectLabel   `json:"labels,omitempty"`
	CreatedAt   time.Time
}

type ServiceSpec struct {
	ID          uint   `json:"id" gorm:"primaryKey"`
	ProjectID   uint   `json:"project_id"`
	Name        string `json:"name"`
	Image       string `json:"image"`
	Command     string `json:"command"`
	VolumeName  string `json:"volume_name"`
	VolumePath  string `json:"volume_path"`
	NetworksRaw string `json:"networks" gorm:"column:networks_raw"`
	LabelsRaw   string `json:"labels" gorm:"column:labels_raw"`
	CreatedAt   time.Time
}

type Deployment struct {
	ID         uint                      `json:"id" gorm:"primaryKey"`
	ProjectID  uint                      `json:"project_id"`
	Project    Project                   `json:"project,omitempty"`
	ServerID   uint                      `json:"server_id"`
	Server     Server                    `json:"server,omitempty"`
	Name       string                    `json:"name"`
	Status     string                    `json:"status"`
	Containers []DeploymentServiceConfig `json:"containers,omitempty"`
	CreatedAt  time.Time
}

type DeploymentServiceConfig struct {
	ID              uint   `json:"id" gorm:"primaryKey"`
	DeploymentID    uint   `json:"deployment_id"`
	ServiceSpecID   uint   `json:"service_spec_id"`
	ServiceName     string `json:"service_name"`
	ContainerID     string `json:"container_id"`
	ContainerName   string `json:"container_name"`
	EnvJSON         string `json:"env_json"`
	PortMappingsRaw string `json:"port_mappings"`
	State           string `json:"state"`
	CreatedAt       time.Time
}

type ProjectNetwork struct {
	ID        uint   `json:"id" gorm:"primaryKey"`
	ProjectID uint   `json:"project_id"`
	Name      string `json:"name"`
	Driver    string `json:"driver"`
	Subnet    string `json:"subnet"`
	Gateway   string `json:"gateway"`
	CreatedAt time.Time
}

type ProjectSecret struct {
	ID        uint   `json:"id" gorm:"primaryKey"`
	ProjectID uint   `json:"project_id"`
	Name      string `json:"name"`
	CreatedAt time.Time
}

type ProjectLabel struct {
	ID        uint   `json:"id" gorm:"primaryKey"`
	ProjectID uint   `json:"project_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	CreatedAt time.Time
}

type User struct {
	ID        uint   `json:"id" gorm:"primaryKey"`
	Username  string `json:"username" gorm:"uniqueIndex"`
	Password  string `json:"-"` // Never return password in JSON
	CreatedAt time.Time
}
