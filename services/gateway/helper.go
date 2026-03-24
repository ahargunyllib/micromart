package main

import (
	"encoding/json"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		http.Error(w, "failed to encode JSON", http.StatusInternalServerError)
		return
	}
}

func writeError(w http.ResponseWriter, statusCode int, msg string) {
	writeJSON(w, statusCode, ErrorResponse{
		Error: msg,
	})
}

func grpcToHTTP(err error) (int, string) {
	st := status.Convert(err)

	switch st.Code() {
	case codes.InvalidArgument:
		return http.StatusBadRequest, st.Message()
	case codes.NotFound:
		return http.StatusNotFound, st.Message()
	case codes.AlreadyExists:
		return http.StatusConflict, st.Message()
	case codes.FailedPrecondition:
		return http.StatusUnprocessableEntity, st.Message()
	case codes.Unauthenticated:
		return http.StatusUnauthorized, st.Message()
	case codes.PermissionDenied:
		return http.StatusForbidden, st.Message()
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests, st.Message()
	case codes.Unavailable:
		return http.StatusServiceUnavailable, st.Message()
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout, st.Message()
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}

func grpcErrorToHTTP(w http.ResponseWriter, err error) {
	code, msg := grpcToHTTP(err)
	writeError(w, code, msg)
}
