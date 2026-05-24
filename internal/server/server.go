package server

import (
	"net/http"
	"time"

	"github.com/drywaters/dejaview/internal/config"
	"github.com/drywaters/dejaview/internal/handler"
	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/session"
	"github.com/drywaters/dejaview/internal/tmdb"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Server represents the HTTP server
type Server struct {
	cfg        *config.Config
	movieRepo  *repository.MovieRepository
	entryRepo  *repository.EntryRepository
	personRepo *repository.PersonRepository
	ratingRepo *repository.RatingRepository
	statsRepo  *repository.StatsRepository
	tmdbClient *tmdb.Client
}

// New creates a new Server
func New(
	cfg *config.Config,
	movieRepo *repository.MovieRepository,
	entryRepo *repository.EntryRepository,
	personRepo *repository.PersonRepository,
	ratingRepo *repository.RatingRepository,
	statsRepo *repository.StatsRepository,
	tmdbClient *tmdb.Client,
) *Server {
	return &Server{
		cfg:        cfg,
		movieRepo:  movieRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
		ratingRepo: ratingRepo,
		statsRepo:  statsRepo,
		tmdbClient: tmdbClient,
	}
}

// Router returns the configured chi router
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.Logger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.SameOrigin)

	// Static files
	const staticCacheControl = "public, max-age=86400"
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", withCacheControl(staticCacheControl, http.StripPrefix("/static/", fileServer)))

	// Root-level static files
	for _, file := range []string{
		"favicon.ico",
		"apple-touch-icon.png",
		"favicon-16x16.png",
		"favicon-32x32.png",
		"android-chrome-192x192.png",
		"android-chrome-512x512.png",
		"site.webmanifest",
	} {
		r.Get("/"+file, serveStaticFile("static/"+file))
	}

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Auth handlers
	sessionManager := session.NewManager(s.cfg.APIToken, 90*24*time.Hour, s.cfg.SecureCookies)
	authHandler := handler.NewAuthHandler(s.cfg.APIToken, sessionManager)
	r.Get("/login", authHandler.LoginPage)
	r.Post("/login", authHandler.Login)
	r.Post("/logout", authHandler.Logout)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(sessionManager))

		// Dashboard
		dashboardHandler := handler.NewDashboardHandler(s.entryRepo, s.personRepo, sessionManager)
		r.Get("/", dashboardHandler.DashboardPage)
		r.Get("/dashboard-content", dashboardHandler.DashboardContent)

		// Stats
		statsHandler := handler.NewStatsHandler(s.statsRepo, sessionManager)
		r.Get("/stats", statsHandler.StatsPage)

		// Movie detail page
		movieHandler := handler.NewMovieHandler(s.movieRepo, s.entryRepo, s.personRepo, s.tmdbClient, sessionManager)
		r.Get("/movies/{id}", movieHandler.MovieDetailPage)

		// TMDB API endpoints
		r.Get("/api/tmdb/search", movieHandler.SearchTMDB)
		r.Post("/api/tmdb/add", movieHandler.AddFromTMDB)

		// Entry API endpoints
		entryHandler := handler.NewEntryHandler(s.entryRepo, s.personRepo, sessionManager)
		r.Put("/api/entries/{id}", entryHandler.Update)
		r.Delete("/api/entries/{id}", entryHandler.Delete)

		// Group partial and reordering
		r.Get("/partials/group/{num}", entryHandler.GroupPartial)
		r.Post("/api/groups/{num}/reorder", entryHandler.Reorder)

		// Rating API endpoints
		ratingHandler := handler.NewRatingHandler(s.ratingRepo, s.entryRepo, s.personRepo, sessionManager)
		r.Put("/api/entries/{id}/ratings", ratingHandler.SaveRatings)
	})

	return r
}

func serveStaticFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

func withCacheControl(cacheControl string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		next.ServeHTTP(w, r)
	})
}
