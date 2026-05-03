package routes

import (
	"github.com/pedroborgesdev/papoql/internal/api/controllers"
	"github.com/pedroborgesdev/papoql/internal/api/middlewares"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(router *gin.Engine) {
	controller := controllers.NewController()

	v1 := router.Group("/v1")

	v1.POST("/prompt", middlewares.SessionMiddleware(), middlewares.MaxBodySize(middlewares.MaxRequestBodyBytes), controller.Prompt)
	v1.POST("/prompt/stream", middlewares.SessionMiddleware(), middlewares.MaxBodySize(middlewares.MaxRequestBodyBytes), controller.PromptStream)
	v1.POST("/prompt/cancel", middlewares.SessionMiddleware(), controller.PromptCancel)
	v1.DELETE("/context", middlewares.SessionMiddleware(), controller.ClearContext)
	v1.GET("/model", controller.ModelGET)
	v1.PUT("/model", controller.ModelPUT)
	v1.POST("/schema", controller.SchemaPOST)
	v1.GET("/schema", controller.SchemaGET)
}
