package store

import (
	"fmt"
	"os"
	"path/filepath"

	"api-go/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func NewSQLite(dbPath string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Migrate User first to ensure it exists
	if err := db.AutoMigrate(&models.User{}); err != nil {
		return nil, err
	}
	fmt.Println("Users table migrated successfully")

	if err := db.AutoMigrate(
		&models.Server{},
		&models.Project{},
		&models.ServiceSpec{},
		&models.Deployment{},
		&models.DeploymentServiceConfig{},
		&models.ProjectNetwork{},
		&models.ProjectSecret{},
		&models.ProjectLabel{},
	); err != nil {
		return nil, err
	}
	
	fmt.Println("Database migration completed successfully")

	var count int64
	if err := db.Model(&models.Server{}).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		local := models.Server{
			Name:       "local",
			DockerHost: "",
			IsLocal:    true,
		}
		if err := db.Create(&local).Error; err != nil {
			return nil, err
		}
	}

	return db, nil
}
