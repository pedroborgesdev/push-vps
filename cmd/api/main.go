package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pedroborgesdev/papoql/internal/api/banner"
	"github.com/pedroborgesdev/papoql/internal/api/config"
	"github.com/pedroborgesdev/papoql/internal/api/logger"
	"github.com/pedroborgesdev/papoql/internal/api/middlewares"
	"github.com/pedroborgesdev/papoql/internal/api/routes"
)

func main() {
	banner.PrintStartup()

	err := config.LoadAppConfig()
	if err != nil {
		logger.Fatalf("Failed to load enviroinments variables", []logger.ParamPair{{Key: "error", Value: err.Error()}})
		return
	}

	logger.Infof("Application has been started", nil)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	router.Use(
		gin.Recovery(),
		middlewares.CORSMiddleware(),
	)

	routes.SetupRoutes(router)

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/icon.png", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "icon.png")
		})
		mux.Handle("/", http.FileServer(http.Dir(".")))
		logger.Infof("Frontend available", []logger.ParamPair{{Key: "url", Value: "http://localhost:8802"}})
		if err := http.ListenAndServe(":8802", mux); err != nil {
			logger.Errorf("Frontend server error", []logger.ParamPair{{Key: "error", Value: err.Error()}})
		}
	}()

	router.Run(":" + config.AppConfig.HTTP_PORT)
}
