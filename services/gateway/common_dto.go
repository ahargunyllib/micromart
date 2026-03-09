package main

type PaginatedResponse[T any] struct {
	Data          []T    `json:"data"`
	NextPageToken string `json:"next_page_token,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
