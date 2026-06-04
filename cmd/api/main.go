package main

import (
	"fmt"

	"api-go/internal/config"
	httpapi "api-go/internal/http"
	"api-go/internal/logger"
	"api-go/internal/service"
	"api-go/internal/store"
)

func main() {
	cfg := config.Load()
	log := logger.New()

	db, err := store.NewSQLite(cfg.DBPath)
	if err != nil {
		panic(err)
	}

	deployments := service.NewDeploymentService(db)
	handler := httpapi.NewHandler(db, deployments)
	router := httpapi.NewRouter(handler)

	log.Info().Str("port", cfg.Port).Msg("api started")
	if err := router.Run(fmt.Sprintf(":%s", cfg.Port)); err != nil {
		panic(err)
	}
}
