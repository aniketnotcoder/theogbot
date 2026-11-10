package wistia

import "errors"

var (
	ErrUploadTokenRequired = errors.New("Wistia upload token is required")
	ErrDataTokenRequired   = errors.New("Wistia Data API read token is required")
	ErrFolderRequired      = errors.New("Wistia folder ID is required")
	ErrInvalidMedia        = errors.New("Telegram message does not contain a supported video document")
	ErrProtectedMedia      = errors.New("Telegram marks this message as protected")
	ErrUploadRejected      = errors.New("Wistia rejected the upload")
	ErrUploadOutcomeUnknown = errors.New("Wistia upload outcome is unknown; reconcile before retrying")
	ErrMediaProcessingFailed = errors.New("Wistia media processing failed")
)
