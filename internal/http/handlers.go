package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	dockerclient "api-go/internal/docker"
	"api-go/internal/middleware"
	"api-go/internal/models"
	"api-go/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Handler struct {
	db          *gorm.DB
	deployments *service.DeploymentService
}

func NewHandler(db *gorm.DB, deployments *service.DeploymentService) *Handler {
	return &Handler{db: db, deployments: deployments}
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) CreateProject(c *gin.Context) {
	var body models.Project
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project name is required"})
		return
	}

	if err := h.db.Create(&body).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, body)
}

func (h *Handler) ListProjects(c *gin.Context) {
	var projects []models.Project
	if err := h.db.Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, projects)
}

func (h *Handler) GetProject(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var project models.Project
	if err := h.db.Preload("Services").Preload("Networks").Preload("Secrets").Preload("Labels").First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	c.JSON(http.StatusOK, project)
}

func (h *Handler) AddServiceToProject(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var project models.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	type reqBody struct {
		Name       string            `json:"name"`
		Image      string            `json:"image"`
		Command    string            `json:"command"`
		VolumeName string            `json:"volume_name"`
		VolumePath string            `json:"volume_path"`
		Networks   []string          `json:"networks"`
		Labels     map[string]string `json:"labels"`
	}
	var body reqBody
	var svc models.ServiceSpec
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Image) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service name and image are required"})
		return
	}
	networksRaw, _ := json.Marshal(body.Networks)
	labelsRaw, _ := json.Marshal(body.Labels)
	svc = models.ServiceSpec{
		ProjectID:   project.ID,
		Name:        body.Name,
		Image:       body.Image,
		Command:     body.Command,
		VolumeName:  body.VolumeName,
		VolumePath:  body.VolumePath,
		NetworksRaw: string(networksRaw),
		LabelsRaw:   string(labelsRaw),
	}
	if err := h.db.Create(&svc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, svc)
}

func (h *Handler) DeployProject(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req service.DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deployment name is required"})
		return
	}
	if req.ServerID != 0 {
		var server models.Server
		if err := h.db.First(&server, req.ServerID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
			return
		}
	}

	deployment, err := h.deployments.DeployProject(c.Request.Context(), uint(id), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, deployment)
}

func (h *Handler) ListDeployments(c *gin.Context) {
	var deployments []models.Deployment
	if err := h.db.Preload("Server").Find(&deployments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, deployments)
}

func (h *Handler) GetDeployment(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var deployment models.Deployment
	if err := h.db.Preload("Containers").Preload("Server").Preload("Project").First(&deployment, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}
	c.JSON(http.StatusOK, deployment)
}

func (h *Handler) GetDeploymentStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	status, err := h.deployments.RefreshDeploymentStatus(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

type scaleDeploymentRequest struct {
	Service  string         `json:"service"`
	Replicas int            `json:"replicas"`
	Many     map[string]int `json:"replicas_by_service"`
}

func (h *Handler) ScaleDeployment(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req scaleDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Many) > 0 {
		deployment, err := h.deployments.ScaleServices(c.Request.Context(), uint(id), req.Many)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, deployment)
		return
	}
	if strings.TrimSpace(req.Service) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service is required"})
		return
	}
	if req.Replicas < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "replicas must be >= 0"})
		return
	}

	deployment, err := h.deployments.ScaleService(c.Request.Context(), uint(id), req.Service, req.Replicas)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, deployment)
}

type pullImageRequest struct {
	Image    string `json:"image"`
	ServerID uint   `json:"server_id"`
}

func (h *Handler) PullImage(c *gin.Context) {
	var req pullImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Image) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image is required"})
		return
	}
	docker, err := h.resolveDockerClient(req.ServerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := docker.PullImage(c.Request.Context(), req.Image); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "image pulled", "image": req.Image})
}

type buildImageRequest struct {
	ContextDir string `json:"context_dir"`
	Tag        string `json:"tag"`
	ServerID   uint   `json:"server_id"`
}

func (h *Handler) BuildImage(c *gin.Context) {
	var req buildImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.ContextDir) == "" || strings.TrimSpace(req.Tag) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "context_dir and tag are required"})
		return
	}
	docker, err := h.resolveDockerClient(req.ServerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := docker.BuildImage(c.Request.Context(), req.ContextDir, req.Tag); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "image built", "tag": req.Tag})
}

func (h *Handler) CreateServer(c *gin.Context) {
	var server models.Server
	if err := c.ShouldBindJSON(&server); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(server.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server name is required"})
		return
	}
	if server.IsLocal {
		server.DockerHost = ""
	}
	if !server.IsLocal && strings.TrimSpace(server.DockerHost) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "docker_host is required for remote server"})
		return
	}
	if err := h.db.Create(&server).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, server)
}

func (h *Handler) AddProjectNetwork(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var project models.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	var network models.ProjectNetwork
	if err := c.ShouldBindJSON(&network); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(network.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "network name is required"})
		return
	}
	if strings.TrimSpace(network.Driver) == "" {
		network.Driver = "bridge"
	}
	network.ProjectID = project.ID
	if err := h.db.Create(&network).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, network)
}

func (h *Handler) AddProjectSecret(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var project models.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	var secret models.ProjectSecret
	if err := c.ShouldBindJSON(&secret); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(secret.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "secret name is required"})
		return
	}
	secret.ProjectID = project.ID
	if err := h.db.Create(&secret).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, secret)
}

func (h *Handler) AddProjectLabel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var project models.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	var label models.ProjectLabel
	if err := c.ShouldBindJSON(&label); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(label.Key) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label key is required"})
		return
	}
	label.ProjectID = project.ID
	if err := h.db.Create(&label).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, label)
}

func (h *Handler) ListServers(c *gin.Context) {
	var servers []models.Server
	if err := h.db.Find(&servers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, servers)
}

func (h *Handler) GetServer(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var server models.Server
	if err := h.db.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}
	c.JSON(http.StatusOK, server)
}

func (h *Handler) resolveDockerClient(serverID uint) (*dockerclient.Client, error) {
	server, err := h.resolveTargetServer(serverID)
	if err != nil {
		return nil, err
	}
	return dockerclient.NewWithHost(server.DockerHost)
}

func (h *Handler) resolveTargetServer(serverID uint) (models.Server, error) {
	var server models.Server
	if serverID != 0 {
		if err := h.db.First(&server, serverID).Error; err != nil {
			return server, errors.New("target server not found")
		}
		return server, nil
	}

	if err := h.db.Where("is_local = ?", true).First(&server).Error; err == nil {
		return server, nil
	}
	if err := h.db.First(&server).Error; err != nil {
		return server, fmt.Errorf("no server configured")
	}
	return server, nil
}

func (h *Handler) Register(c *gin.Context) {
	type reqBody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	var body reqBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Username) == "" || strings.TrimSpace(body.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	user := models.User{
		Username: body.Username,
		Password: string(hashedPassword),
	}
	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	token, err := middleware.GenerateToken(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"token": token, "user_id": user.ID})
}

func (h *Handler) Login(c *gin.Context) {
	type reqBody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	var body reqBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Username) == "" || strings.TrimSpace(body.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	var user models.User
	if err := h.db.Where("username = ?", body.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := middleware.GenerateToken(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "user_id": user.ID})
}
