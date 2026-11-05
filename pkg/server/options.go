package server

import "net/http"

type SystemServerOption func(*SystemServer)

func WithSystemHandlers(handlers ...map[string]http.HandlerFunc) SystemServerOption {
	return func(s *SystemServer) {
		s.systemHandlers = append(s.systemHandlers, handlers...)
	}
}
