package config

import (
	"github.com/TechAlkurn/core/docs"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// RunGRPC starts a gRPC server on the specified port.
func InitSwagger(r *gin.Engine) {
	docs.SwaggerInfo.Title = "Openwindow API"
	docs.SwaggerInfo.Description = "This is a Openwindow server."
	docs.SwaggerInfo.Version = "1.0"
	docs.SwaggerInfo.Host = "localhost:8000"
	docs.SwaggerInfo.BasePath = "/"
	docs.SwaggerInfo.Schemes = []string{"http", "https"}
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}
