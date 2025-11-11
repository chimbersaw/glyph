package app

import (
	"go-glyph/configuration"
	"go-glyph/internal/api/middleware"
	"go-glyph/internal/core/services"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func Run(c *configuration.EnvConfigModel) {
	//db := database.ConnectDB(c)

	//glyphRepository := repository.NewGlyphRepository(db)

	//glyphService := services.NewGlyphService(glyphRepository)
	// stratzService := services.NewStratzService(c.STRATZToken)
	// opendotaService := services.NewOpendotaService()
	goSteamService := services.NewGoSteamService(c.SteamLoginUsernames, c.SteamLoginPasswords)
	//valveService := services.NewValveService()
	//mantaService := services.NewMantaService()

	//glyphController := controllers.NewGlyphController(glyphService, goSteamService, valveService, mantaService)

	//glyphRouter := routers.NewGlyphRouter(glyphController)

	app := fiber.New(fiber.Config{
		ErrorHandler:            middleware.ErrorHandler,
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"127.0.0.1", "::1"},
	})

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

	//routers.SetupRoutes(app, glyphRouter)

	port := c.Port
	if port == "" {
		port = "8000"
	}

	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}

	log.Printf("Starting server on port %s", port)
	go goSteamService.GetMatchDetails(1107999448)
	log.Fatal(app.Listen(host + ":" + port))
}
