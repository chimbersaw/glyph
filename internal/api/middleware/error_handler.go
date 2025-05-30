package middleware

import (
	"github.com/gofiber/fiber/v2"
	"go-glyph/internal/core/dtos"
	"go-glyph/internal/core/services"
)

func ErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	message := "Internal Server Error"

	switch e := err.(type) {
	case services.UserFacingError:
		return c.Status(e.Code).JSON(dtos.MessageResponseType{Message: e.Message})
	case services.ValidateError:
		code = fiber.StatusBadRequest
		message = e.Error()
	case services.RepositoryError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.MatchAlreadyParsedError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.FileCreationError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.FolderCreationError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.CopyError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.GETError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.ReadResponseBodyError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.HTTPError:
		code = fiber.StatusNotFound
		message = e.Error()
	case services.ParserCreationError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.ParserError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.OpenFileError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.RemoveFileError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	case services.CloseFileError:
		code = fiber.StatusInternalServerError
		message = e.Error()
	default:
		message = err.Error()
	}

	err = c.Status(code).JSON(fiber.Map{"message": message})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Cannot send error JSON message")
	}

	return nil
}
