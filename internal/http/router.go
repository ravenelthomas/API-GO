package http

import (
	"api-go/internal/middleware"

	"github.com/gin-gonic/gin"
)

func NewRouter(h *Handler) *gin.Engine {
	r := gin.Default()

	// Public routes
	r.GET("/health", h.Health)
	r.POST("/auth/register", h.Register)
	r.POST("/auth/login", h.Login)

	// Protected routes
	protected := r.Group("/")
	protected.Use(middleware.AuthMiddleware())
	{
		protected.POST("/servers", h.CreateServer)
		protected.GET("/servers", h.ListServers)
		protected.GET("/servers/:id", h.GetServer)

		protected.POST("/images/pull", h.PullImage)
		protected.POST("/images/build", h.BuildImage)

		protected.POST("/projects", h.CreateProject)
		protected.GET("/projects", h.ListProjects)
		protected.GET("/projects/:id", h.GetProject)
		protected.POST("/projects/:id/services", h.AddServiceToProject)
		protected.POST("/projects/:id/networks", h.AddProjectNetwork)
		protected.POST("/projects/:id/secrets", h.AddProjectSecret)
		protected.POST("/projects/:id/labels", h.AddProjectLabel)
		protected.POST("/projects/:id/deployments", h.DeployProject)

		protected.GET("/deployments", h.ListDeployments)
		protected.GET("/deployments/:id", h.GetDeployment)
		protected.GET("/deployments/:id/status", h.GetDeploymentStatus)
		protected.POST("/deployments/:id/scale", h.ScaleDeployment)
	}

	return r
}
