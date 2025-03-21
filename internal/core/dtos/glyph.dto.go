package dtos

import "go-glyph/internal/core/models"

type Match struct {
	ID         int `validate:"required"`
	Cluster    int `validate:"required"`
	ReplaySalt int `validate:"required"`
}

type GetGlyphs struct {
	MatchID int `validate:"required"`
}

type CreateGlyphs struct {
	Glyphs []models.Glyph
}

type GlyphParse struct {
	GlyphParsed bool
	Glyphs      []models.Glyph
}

type HeroPlayer struct {
	HeroID   uint32
	PlayerID uint64
}
