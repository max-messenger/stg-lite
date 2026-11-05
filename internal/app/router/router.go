package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"lite/pkg/http/middlewares"
)

// Controller api controller.
type Controller func(r chi.Router)

func NewRouter(
	logger *zap.Logger,
	controllers ...Controller,
) (http.Handler, error) {
	router := chi.NewRouter()
	router.Route("/", func(r chi.Router) {
		r.Use(
			middlewares.Metrics(),
			middlewares.AccessLog(logger),
			middlewares.Trace(),
		)
		for _, ctrl := range controllers {
			ctrl(r)
		}
	})

	return router, nil
}
