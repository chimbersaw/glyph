package main

import (
	"go-glyph/configuration"
	"go-glyph/internal/api/app"
	"log"
)

// @title           Glyph Dota 2 REST API
// @version         1.0
// @description     Go Glyph REST API

// @host      localhost:8000
func main() {
	err := configuration.LoadConfig(".env")
	if err != nil {
		log.Fatalln("Failed to load environment variables!", err.Error())
	}
	app.Run(&configuration.EnvConfig)
}
