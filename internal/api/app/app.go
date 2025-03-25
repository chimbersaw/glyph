package app

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"go-glyph/configuration"
	"go-glyph/internal/api/controllers"
	"go-glyph/internal/api/middleware"
	"go-glyph/internal/api/routers"
	"go-glyph/internal/core/services"
	"go-glyph/internal/data/database"
	"go-glyph/internal/data/repository"
	"log"
)

func Run(c *configuration.EnvConfigModel) {
	db := database.ConnectDB(c)

	glyphRepository := repository.NewGlyphRepository(db)

	glyphService := services.NewGlyphService(glyphRepository)
	// stratzService := services.NewStratzService(c.STRATZToken)
	// opendotaService := services.NewOpendotaService()
	goSteamService := services.NewGoSteamService(c.SteamLoginUsernames, c.SteamLoginPasswords)
	valveService := services.NewValveService()
	mantaService := services.NewMantaService()

	glyphController := controllers.NewGlyphController(glyphService, goSteamService, valveService, mantaService)

	glyphRouter := routers.NewGlyphRouter(glyphController)

	app := fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler})

	//	Logger middleware for logging HTTP request/response details
	app.Use(logger.New())

	//	CORS middleware
	allowedOrigins := "http://127.0.0.1:8000,http://localhost:5173,http://localhost:4173"
	if c.CorsAllowedOrigins != "" {
		allowedOrigins += "," + c.CorsAllowedOrigins
	}

	app.Use(cors.New(cors.Config{
		AllowOrigins: allowedOrigins,
		AllowHeaders: "POST",
	}))

	routers.SetupRoutes(app, glyphRouter)

	port := c.Port
	if port == "" {
		port = "8000"
	}

	log.Fatal(app.Listen(":" + port))
}
