package response

import (
	"encoding/json"
	"net/http"
)

// ChaliceError mirrors the Chalice error shape that the frontend expects:
//
//	{"Code": "BadRequestError", "Message": "something went wrong"}
type ChaliceError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func WriteError(w http.ResponseWriter, code string, message string, status int) {
	WriteJSON(w, status, ChaliceError{Code: code, Message: message})
}

func BadRequest(w http.ResponseWriter, msg string) {
	WriteError(w, "BadRequestError", msg, http.StatusBadRequest)
}

func Unauthorized(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "Unauthorized"
	}
	WriteError(w, "UnauthorizedError", msg, http.StatusUnauthorized)
}

func Forbidden(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "Forbidden"
	}
	WriteError(w, "ForbiddenError", msg, http.StatusForbidden)
}

func NotFound(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "Not Found"
	}
	WriteError(w, "NotFoundError", msg, http.StatusNotFound)
}

func InternalError(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "Internal Server Error"
	}
	WriteError(w, "InternalServerError", msg, http.StatusInternalServerError)
}
