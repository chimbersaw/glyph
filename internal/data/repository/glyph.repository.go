package repository

import (
	"go-glyph/internal/core/models"
	"gorm.io/gorm"
)

type GlyphRepository struct {
	db *gorm.DB
}

func NewGlyphRepository(db *gorm.DB) *GlyphRepository {
	return &GlyphRepository{db: db}
}

func (r *GlyphRepository) GetGlyphs(matchID int) ([]models.Glyph, error) {
	var glyphs []models.Glyph
	record := r.db.Where("match_id = ?", matchID).Find(&glyphs)
	return glyphs, record.Error
}

func (r *GlyphRepository) GlyphsExist(matchID int) (bool, error) {
	var count int64
	result := r.db.Model(&models.Glyph{}).Where("match_id = ?", matchID).Count(&count)

	if result.Error != nil {
		return false, result.Error
	}

	return count > 0, nil
}

func (r *GlyphRepository) CreateGlyphs(newGlyphs []models.Glyph) error {
	record := r.db.Create(newGlyphs)
	return record.Error
}
