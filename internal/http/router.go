package http

import "github.com/gin-gonic/gin"

func NewRouter(h *Handler) *gin.Engine {
	r := gin.Default()

	r.GET("/health", h.Health)

	r.POST("/servers", h.CreateServer)
	r.GET("/servers", h.ListServers)
	r.GET("/servers/:id", h.GetServer)

	r.POST("/images/pull", h.PullImage)
	r.POST("/images/build", h.BuildImage)

	r.POST("/projects", h.CreateProject)
	r.GET("/projects", h.ListProjects)
	r.GET("/projects/:id", h.GetProject)
	r.POST("/projects/:id/services", h.AddServiceToProject)
	r.POST("/projects/:id/networks", h.AddProjectNetwork)
	r.POST("/projects/:id/secrets", h.AddProjectSecret)
	r.POST("/projects/:id/labels", h.AddProjectLabel)
	r.POST("/projects/:id/deployments", h.DeployProject)

	r.GET("/deployments", h.ListDeployments)
	r.GET("/deployments/:id", h.GetDeployment)
	r.GET("/deployments/:id/status", h.GetDeploymentStatus)
	r.POST("/deployments/:id/scale", h.ScaleDeployment)

	return r
}
