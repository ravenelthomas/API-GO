package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	dockerclient "api-go/internal/docker"
	"api-go/internal/models"

	"gorm.io/gorm"
)

type DeploymentService struct {
	db *gorm.DB
}

func NewDeploymentService(db *gorm.DB) *DeploymentService {
	return &DeploymentService{db: db}
}

type DeployRequest struct {
	Name            string                       `json:"name"`
	ServerID        uint                         `json:"server_id"`
	EnvOverrides    map[string]map[string]string `json:"env_overrides"`
	SecretOverrides map[string]map[string]string `json:"secret_overrides"`
	PortMappings    map[string]map[string]string `json:"port_mappings"`
	Labels          map[string]map[string]string `json:"labels"`
	Replicas        map[string]int               `json:"replicas"`
}

func (s *DeploymentService) DeployProject(ctx context.Context, projectID uint, req DeployRequest) (*models.Deployment, error) {
	var project models.Project
	if err := s.db.Preload("Services").Preload("Networks").Preload("Secrets").Preload("Labels").First(&project, projectID).Error; err != nil {
		return nil, err
	}

	server, err := s.resolveTargetServer(req.ServerID)
	if err != nil {
		return nil, err
	}
	docker, err := dockerclient.NewWithHost(server.DockerHost)
	if err != nil {
		return nil, err
	}

	deployment := models.Deployment{
		ProjectID: project.ID,
		ServerID:  server.ID,
		Name:      req.Name,
		Status:    "not-running",
	}
	if deployment.Name == "" {
		deployment.Name = fmt.Sprintf("%s-deployment", project.Name)
	}
	if err := s.db.Create(&deployment).Error; err != nil {
		return nil, err
	}

	runningCount := 0
	targetCount := 0
	for _, svc := range project.Services {
		replicas := req.Replicas[svc.Name]
		if replicas <= 0 {
			replicas = 1
		}

		targetCount += replicas
		_ = docker.PullImage(ctx, svc.Image)
		_ = docker.EnsureVolume(ctx, svc.VolumeName)

		env := []string{}
		for k, v := range req.EnvOverrides[svc.Name] {
			env = append(env, k+"="+v)
		}
		for _, secret := range project.Secrets {
			if value, ok := req.SecretOverrides[svc.Name][secret.Name]; ok {
				_ = docker.CreateSecretIfSupported(ctx, fmt.Sprintf("%s_%s", project.Name, secret.Name), value)
				env = append(env, strings.ToUpper(secret.Name)+"="+value)
			}
		}

		labels := map[string]string{
			"mds.project":    project.Name,
			"mds.deployment": deployment.Name,
			"mds.service":    svc.Name,
		}
		for _, label := range project.Labels {
			labels[label.Key] = label.Value
		}
		for key, value := range parseServiceLabels(svc.LabelsRaw) {
			labels[key] = value
		}
		for key, value := range req.Labels[svc.Name] {
			labels[key] = value
		}

		binds := []string{}
		if svc.VolumeName != "" && svc.VolumePath != "" {
			binds = append(binds, fmt.Sprintf("%s:%s", svc.VolumeName, svc.VolumePath))
		}

		command := []string{}
		if strings.TrimSpace(svc.Command) != "" {
			command = strings.Split(svc.Command, " ")
		}

		for _, netw := range project.Networks {
			_ = docker.EnsureNetwork(ctx, fmt.Sprintf("%s_%s", project.Name, netw.Name), netw.Driver, netw.Subnet, netw.Gateway)
		}

		for i := 0; i < replicas; i++ {
			portMappings := withReplicaPortOffset(req.PortMappings[svc.Name], i)
			containerID, containerName, err := docker.CreateAndStartContainer(ctx, dockerclient.CreateContainerRequest{
				Name:         fmt.Sprintf("%s-%s-%d", deployment.Name, svc.Name, i+1),
				Image:        svc.Image,
				Command:      command,
				Env:          env,
				Labels:       labels,
				Binds:        binds,
				PortBindings: portMappings,
			})
			for _, netw := range parseServiceNetworks(svc.NetworksRaw) {
				_ = docker.ConnectContainerToNetwork(ctx, containerID, fmt.Sprintf("%s_%s", project.Name, netw))
			}

			state := "error"
			if err == nil {
				state = "running"
				runningCount++
			}
			if err != nil {
				containerName = err.Error()
			}

			envRaw, _ := json.Marshal(req.EnvOverrides[svc.Name])
			portsRaw, _ := json.Marshal(portMappings)

			link := models.DeploymentServiceConfig{
				DeploymentID:    deployment.ID,
				ServiceSpecID:   svc.ID,
				ServiceName:     svc.Name,
				ContainerID:     containerID,
				ContainerName:   strings.TrimPrefix(containerName, "/"),
				EnvJSON:         string(envRaw),
				PortMappingsRaw: string(portsRaw),
				State:           state,
			}
			if err := s.db.Create(&link).Error; err != nil {
				return nil, err
			}
		}
	}

	switch {
	case runningCount == 0:
		deployment.Status = "not-running"
	case runningCount == targetCount:
		deployment.Status = "running"
	default:
		deployment.Status = "partially-running"
	}

	if err := s.db.Save(&deployment).Error; err != nil {
		return nil, err
	}

	if err := s.db.Preload("Containers").Preload("Server").Preload("Project").First(&deployment, deployment.ID).Error; err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (s *DeploymentService) RefreshDeploymentStatus(ctx context.Context, deploymentID uint) (string, error) {
	var deployment models.Deployment
	if err := s.db.Preload("Containers").Preload("Server").First(&deployment, deploymentID).Error; err != nil {
		return "", err
	}

	docker, err := dockerclient.NewWithHost(deployment.Server.DockerHost)
	if err != nil {
		return "", err
	}

	runningCount := 0
	for _, c := range deployment.Containers {
		running, err := docker.ContainerRunning(ctx, c.ContainerID)
		if err != nil {
			c.State = "error"
		} else if running {
			c.State = "running"
			runningCount++
		} else {
			c.State = "not-running"
		}
		_ = s.db.Save(&c).Error
	}

	switch {
	case runningCount == 0:
		deployment.Status = "not-running"
	case runningCount == len(deployment.Containers):
		deployment.Status = "running"
	default:
		deployment.Status = "partially-running"
	}
	if err := s.db.Save(&deployment).Error; err != nil {
		return "", err
	}

	return deployment.Status, nil
}

func (s *DeploymentService) ScaleService(ctx context.Context, deploymentID uint, serviceName string, replicas int) (*models.Deployment, error) {
	if strings.TrimSpace(serviceName) == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if replicas < 0 {
		return nil, fmt.Errorf("replicas must be >= 0")
	}

	var deployment models.Deployment
	if err := s.db.Preload("Containers").Preload("Server").Preload("Project.Services").Preload("Project.Networks").Preload("Project.Labels").First(&deployment, deploymentID).Error; err != nil {
		return nil, err
	}

	var spec *models.ServiceSpec
	for i := range deployment.Project.Services {
		if deployment.Project.Services[i].Name == serviceName {
			spec = &deployment.Project.Services[i]
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("service %q not found in project", serviceName)
	}

	docker, err := dockerclient.NewWithHost(deployment.Server.DockerHost)
	if err != nil {
		return nil, err
	}

	current := []models.DeploymentServiceConfig{}
	for _, c := range deployment.Containers {
		if c.ServiceName == serviceName {
			current = append(current, c)
		}
	}
	sort.Slice(current, func(i, j int) bool {
		return replicaIndexFromName(current[i].ContainerName) < replicaIndexFromName(current[j].ContainerName)
	})

	if replicas > len(current) {
		baseEnv, basePorts := extractScaleBaseConfig(current)

		_ = docker.PullImage(ctx, spec.Image)
		_ = docker.EnsureVolume(ctx, spec.VolumeName)

		binds := []string{}
		if spec.VolumeName != "" && spec.VolumePath != "" {
			binds = append(binds, fmt.Sprintf("%s:%s", spec.VolumeName, spec.VolumePath))
		}
		command := []string{}
		if strings.TrimSpace(spec.Command) != "" {
			command = strings.Split(spec.Command, " ")
		}
		env := mapToEnv(baseEnv)

		for i := len(current); i < replicas; i++ {
			portMappings := withReplicaPortOffset(basePorts, i)
			containerID, containerName, createErr := docker.CreateAndStartContainer(ctx, dockerclient.CreateContainerRequest{
				Name:         fmt.Sprintf("%s-%s-%d", deployment.Name, serviceName, i+1),
				Image:        spec.Image,
				Command:      command,
				Env:          env,
				Labels:       projectLabels(deployment.Project.Labels, deployment.Project.Name, deployment.Name, serviceName),
				Binds:        binds,
				PortBindings: portMappings,
			})
			for _, netw := range parseServiceNetworks(spec.NetworksRaw) {
				_ = docker.ConnectContainerToNetwork(ctx, containerID, fmt.Sprintf("%s_%s", deployment.Project.Name, netw))
			}

			state := "error"
			if createErr == nil {
				state = "running"
			} else {
				containerName = createErr.Error()
			}

			envRaw, _ := json.Marshal(baseEnv)
			portsRaw, _ := json.Marshal(portMappings)
			link := models.DeploymentServiceConfig{
				DeploymentID:    deployment.ID,
				ServiceSpecID:   spec.ID,
				ServiceName:     serviceName,
				ContainerID:     containerID,
				ContainerName:   strings.TrimPrefix(containerName, "/"),
				EnvJSON:         string(envRaw),
				PortMappingsRaw: string(portsRaw),
				State:           state,
			}
			if err := s.db.Create(&link).Error; err != nil {
				return nil, err
			}
		}
	} else if replicas < len(current) {
		for i := len(current) - 1; i >= replicas; i-- {
			cfg := current[i]
			_ = docker.RemoveContainer(ctx, cfg.ContainerID, true)
			if err := s.db.Delete(&models.DeploymentServiceConfig{}, cfg.ID).Error; err != nil {
				return nil, err
			}
		}
	}

	if _, err := s.RefreshDeploymentStatus(ctx, deployment.ID); err != nil {
		return nil, err
	}

	if err := s.db.Preload("Containers").Preload("Server").Preload("Project").First(&deployment, deployment.ID).Error; err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (s *DeploymentService) ScaleServices(ctx context.Context, deploymentID uint, replicas map[string]int) (*models.Deployment, error) {
	if len(replicas) == 0 {
		return nil, fmt.Errorf("replicas payload is required")
	}
	for serviceName, count := range replicas {
		if _, err := s.ScaleService(ctx, deploymentID, serviceName, count); err != nil {
			return nil, err
		}
	}
	var deployment models.Deployment
	if err := s.db.Preload("Containers").Preload("Server").Preload("Project").First(&deployment, deploymentID).Error; err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (s *DeploymentService) resolveTargetServer(serverID uint) (models.Server, error) {
	var server models.Server
	if serverID != 0 {
		if err := s.db.First(&server, serverID).Error; err != nil {
			return server, fmt.Errorf("target server not found")
		}
		return server, nil
	}

	if err := s.db.Where("is_local = ?", true).First(&server).Error; err == nil {
		return server, nil
	}
	if err := s.db.First(&server).Error; err != nil {
		return server, fmt.Errorf("no server configured")
	}
	return server, nil
}

func withReplicaPortOffset(base map[string]string, replica int) map[string]string {
	if len(base) == 0 {
		return map[string]string{}
	}
	if replica == 0 {
		return base
	}
	out := make(map[string]string, len(base))
	for containerPort, hostPort := range base {
		n, err := strconv.Atoi(hostPort)
		if err != nil {
			out[containerPort] = hostPort
			continue
		}
		out[containerPort] = strconv.Itoa(n + replica)
	}
	return out
}

func replicaIndexFromName(name string) int {
	parts := strings.Split(name, "-")
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	if n <= 0 {
		return 0
	}
	return n - 1
}

func extractScaleBaseConfig(current []models.DeploymentServiceConfig) (map[string]string, map[string]string) {
	if len(current) == 0 {
		return map[string]string{}, map[string]string{}
	}
	env := map[string]string{}
	ports := map[string]string{}
	_ = json.Unmarshal([]byte(current[0].EnvJSON), &env)
	_ = json.Unmarshal([]byte(current[0].PortMappingsRaw), &ports)
	return env, ports
}

func mapToEnv(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for k, v := range values {
		out = append(out, k+"="+v)
	}
	return out
}

func parseServiceNetworks(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func projectLabels(labels []models.ProjectLabel, projectName, deploymentName, serviceName string) map[string]string {
	out := map[string]string{
		"mds.project":    projectName,
		"mds.deployment": deploymentName,
		"mds.service":    serviceName,
	}
	for _, label := range labels {
		out[label.Key] = label.Value
	}
	return out
}

func parseServiceLabels(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]string{}
	}
	return out
}
