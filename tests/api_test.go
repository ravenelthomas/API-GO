package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	httpapi "api-go/internal/http"
	"api-go/internal/logger"
	"api-go/internal/models"
	"api-go/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	router      *gin.Engine
	db          *gorm.DB
	testToken   string
	testUserID  uint
	testServerID uint
	testProjectID uint
)

func setupTestDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("Failed to connect to test database")
	}
	
	// Auto migrate all models
	db.AutoMigrate(
		&models.Server{},
		&models.Project{},
		&models.ServiceSpec{},
		&models.Deployment{},
		&models.DeploymentServiceConfig{},
		&models.ProjectNetwork{},
		&models.ProjectSecret{},
		&models.ProjectLabel{},
		&models.User{},
	)
	
	return db
}

func TestMain(m *testing.M) {
	// Setup
	gin.SetMode(gin.TestMode)
	db = setupTestDB()
	
	log := logger.New()
	
	deployments := service.NewDeploymentService(db)
	handler := httpapi.NewHandler(db, deployments)
	router = httpapi.NewRouter(handler)
	
	log.Info().Msg("Tests setup complete")
	
	// Run tests
	m.Run()
}

func TestHealthEndpoint(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestRegisterUser(t *testing.T) {
	body := map[string]string{
		"username": "testuser",
		"password": "testpass123",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/auth/register", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "token")
	assert.Contains(t, response, "user_id")
	
	testToken = response["token"].(string)
	testUserID = uint(response["user_id"].(float64))
}

func TestRegisterDuplicateUser(t *testing.T) {
	body := map[string]string{
		"username": "testuser",
		"password": "testpass123",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/auth/register", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusConflict, w.Code)
	
	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "username already exists")
}

func TestRegisterInvalidData(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		expected int
	}{
		{"Missing username", "", "password", http.StatusBadRequest},
		{"Missing password", "username", "", http.StatusBadRequest},
		{"Both missing", "", "", http.StatusBadRequest},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]string{
				"username": tt.username,
				"password": tt.password,
			}
			jsonBody, _ := json.Marshal(body)
			
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/auth/register", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			
			assert.Equal(t, tt.expected, w.Code)
		})
	}
}

func TestLoginUser(t *testing.T) {
	body := map[string]string{
		"username": "testuser",
		"password": "testpass123",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/auth/login", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "token")
	assert.Contains(t, response, "user_id")
}

func TestLoginInvalidCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		expected int
	}{
		{"Wrong password", "testuser", "wrongpass", http.StatusUnauthorized},
		{"Non-existent user", "nonexistent", "password", http.StatusUnauthorized},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]string{
				"username": tt.username,
				"password": tt.password,
			}
			jsonBody, _ := json.Marshal(body)
			
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/auth/login", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			
			assert.Equal(t, tt.expected, w.Code)
		})
	}
}

func addAuthHeader(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testToken))
}

func TestProtectedRouteWithoutAuth(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/servers", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	
	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "Authorization header required")
}

func TestProtectedRouteWithInvalidToken(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/servers", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	
	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "Invalid token")
}

func TestCreateServer(t *testing.T) {
	body := map[string]interface{}{
		"name":        "test-server",
		"docker_host": "unix:///var/run/docker.sock",
		"is_local":    true,
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/servers", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var response models.Server
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-server", response.Name)
	assert.True(t, response.IsLocal)
	
	testServerID = response.ID
}

func TestCreateServerInvalidData(t *testing.T) {
	body := map[string]interface{}{
		"name": "", // Missing name
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/servers", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListServers(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/servers", nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response []models.Server
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(response), 1)
}

func TestGetServer(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/servers/%d", testServerID), nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response models.Server
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, testServerID, response.ID)
}

func TestCreateProject(t *testing.T) {
	body := map[string]interface{}{
		"name":        "test-project",
		"description": "Test project for API",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/projects", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var response models.Project
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-project", response.Name)
	
	testProjectID = response.ID
}

func TestCreateProjectInvalidData(t *testing.T) {
	body := map[string]interface{}{
		"name": "", // Missing name
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/projects", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListProjects(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/projects", nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response []models.Project
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(response), 1)
}

func TestGetProject(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/projects/%d", testProjectID), nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response models.Project
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, testProjectID, response.ID)
}

func TestAddServiceToProject(t *testing.T) {
	body := map[string]interface{}{
		"name":        "test-service",
		"image":       "nginx:alpine",
		"volume_name": "test-volume",
		"volume_path": "/data",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/projects/%d/services", testProjectID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var response models.ServiceSpec
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-service", response.Name)
	assert.Equal(t, "nginx:alpine", response.Image)
}

func TestAddServiceInvalidData(t *testing.T) {
	body := map[string]interface{}{
		"name":  "", // Missing name
		"image": "nginx:alpine",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/projects/%d/services", testProjectID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAddProjectNetwork(t *testing.T) {
	body := map[string]interface{}{
		"name":   "test-network",
		"driver": "bridge",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/projects/%d/networks", testProjectID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var response models.ProjectNetwork
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-network", response.Name)
}

func TestAddProjectSecret(t *testing.T) {
	body := map[string]interface{}{
		"name": "test-secret",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/projects/%d/secrets", testProjectID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var response models.ProjectSecret
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test-secret", response.Name)
}

func TestAddProjectLabel(t *testing.T) {
	body := map[string]interface{}{
		"key":   "test.label",
		"value": "test-value",
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/projects/%d/labels", testProjectID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusCreated, w.Code)
	
	var response models.ProjectLabel
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "test.label", response.Key)
}

func TestPullImage(t *testing.T) {
	body := map[string]interface{}{
		"image":     "nginx:alpine",
		"server_id": testServerID,
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/images/pull", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	// This might fail if Docker is not available, but we test the endpoint exists
	// In a real test environment, we would mock the Docker client
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusBadGateway)
}

func TestPullImageInvalidData(t *testing.T) {
	body := map[string]interface{}{
		"image": "", // Missing image
	}
	jsonBody, _ := json.Marshal(body)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/images/pull", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListDeployments(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/deployments", nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response []models.Deployment
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
}

func TestGetNonExistentDeployment(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/deployments/99999", nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetNonExistentServer(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/servers/99999", nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetNonExistentProject(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/projects/99999", nil)
	addAuthHeader(req)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusNotFound, w.Code)
}
