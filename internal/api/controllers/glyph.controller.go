package controllers

import (
	"github.com/gofiber/fiber/v2"
	"go-glyph/internal/core/dtos"
	"go-glyph/internal/core/models"
	"strconv"
	"sync"
)

type GlyphService interface {
	GetGlyphs(getGlyphs *dtos.GetGlyphs) (dtos.GlyphParse, error)
	CreateGlyphs(createGlyphs *dtos.CreateGlyphs) error
}

type GoSteamService interface {
	GetMatchDetails(matchID int) (dtos.Match, error)
}

// type StratzService interface {
// 	GetMatchFromStratzAPI(matchID int) (dtos.Match, error)
// }
//
// type OpendotaService interface {
// 	GetMatchFromOpendotaAPI(matchID int) (dtos.Match, error)
// }

type ValveService interface {
	RetrieveFile(match dtos.Match) error
}

type MantaService interface {
	GetGlyphsFromDem(match dtos.Match) ([]models.Glyph, error)
}

type GlyphController struct {
	GlyphService   GlyphService
	GoSteamService GoSteamService
	// OpendotaService OpendotaService
	// StratzService   StratzService
	ValveService ValveService
	MantaService MantaService

	activeMatches sync.Map
}

func NewGlyphController(glyphService GlyphService, goSteamService GoSteamService,
	// opendotaService OpendotaService, stratzService StratzService,
	valveService ValveService, mantaService MantaService) *GlyphController {
	return &GlyphController{
		GlyphService:   glyphService,
		GoSteamService: goSteamService,
		// OpendotaService: opendotaService,
		// StratzService:   stratzService,
		ValveService:  valveService,
		MantaService:  mantaService,
		activeMatches: sync.Map{},
	}
}

// Returns true if we were able to store (meaning it wasn't already there)
func (cr *GlyphController) markIfNotProcessing(matchID int) bool {
	_, loaded := cr.activeMatches.LoadOrStore(matchID, true)
	return !loaded
}

func (cr *GlyphController) markMatchAsFinished(matchID int) {
	cr.activeMatches.Delete(matchID)
}

// GetGlyphs
//
//	@Summary		Get glyphs
//	@Description	Get glyphs using match id
//	@Tags			glyph
//	@Accept			json
//	@Produce		json
//	@Param			matchID					path		string						true	"Match ID"
//	@Success		200						{object}	[]models.Glyph				"Glyphs from database"
//	@Success		201						{object}	[]models.Glyph				"Glyphs parsed and save to database"
//	@Success		202						{object}	dtos.MessageResponseType	"Match is already being processed"
//	@Failure		400						{object}	dtos.MessageResponseType	"Glyphs parse error"
//	@Router			/api/glyph/{matchID}	[post]
func (cr *GlyphController) GetGlyphs(c *fiber.Ctx) error {
	matchIDString := c.Params("matchID")
	matchID, err := strconv.Atoi(matchIDString)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(
			dtos.MessageResponseType{Message: "Match ID is not an integer"},
		)
	}

	// Check if parsed match is stored in db and retrieve if stored
	getGlyphes := &dtos.GetGlyphs{MatchID: matchID}
	glyphParse, err := cr.GlyphService.GetGlyphs(getGlyphes)
	if err != nil {
		return err
	}

	// If match is parsed -> return parsed match
	if glyphParse.GlyphParsed == true {
		return c.Status(fiber.StatusOK).JSON(glyphParse.Glyphs)
	}

	// // If not in db
	// // Make request to STRATZ API
	// match, err := cr.StratzService.GetMatchFromStratzAPI(matchID)
	// if err.Error() == "API error" {
	// 	match, err = cr.OpendotaService.GetMatchFromOpendotaAPI(matchID)
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	// Atomically try to mark this match as processing
	if !cr.markIfNotProcessing(matchID) {
		return c.Status(fiber.StatusAccepted).JSON(
			dtos.MessageResponseType{Message: "Match is already being processed"},
		)
	}

	// Make sure to mark as finished when we're done
	defer cr.markMatchAsFinished(matchID)

	match, err := cr.GoSteamService.GetMatchDetails(matchID)
	if err != nil {
		return err
	}

	// Download from valve cluster
	err = cr.ValveService.RetrieveFile(match)
	if err != nil {
		return err
	}

	// Parse using Manta(Dotabuff golang parser)
	glyphs, err := cr.MantaService.GetGlyphsFromDem(match)
	if err != nil {
		return err
	}

	// Save parsed match to database
	createGlyphs := dtos.CreateGlyphs{Glyphs: glyphs}
	err = cr.GlyphService.CreateGlyphs(&createGlyphs)
	if err != nil {
		return err
	}

	// Return parsed match
	return c.Status(fiber.StatusCreated).JSON(glyphs)
}
