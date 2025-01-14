package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neicnordic/sda-download/api/s3"
	"github.com/neicnordic/sda-download/api/sda"
	"github.com/neicnordic/sda-download/internal/config"
	log "github.com/sirupsen/logrus"
)

// SelectedMiddleware is used to control authentication and authorization
// behaviour with config app.middleware
// available middlewares:
// "default" for TokenMiddleware
var SelectedMiddleware = func() gin.HandlerFunc {
	return nil
}

// healthResponse
func healthResponse(c *gin.Context) {
	// ok response to health
	c.Writer.WriteHeader(http.StatusOK)
}

// Setup configures the web server and registers the routes
func Setup() *http.Server {
	// Set up routing
	log.Info("(2/5) Registering endpoint handlers")
	if log.GetLevel() != log.DebugLevel {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()

	router.Use(gin.LoggerWithConfig(
		gin.LoggerConfig{
			Formatter: func(params gin.LogFormatterParams) string {
				// log if in debug mode, if a download was requested or if the request failed
				if log.GetLevel() == log.DebugLevel || strings.HasPrefix(params.Path, "/s3/") || params.StatusCode >= 400 {
					s, _ := json.Marshal(map[string]any{
						"level":       "debug",
						"method":      params.Method,
						"path":        params.Path,
						"remote_addr": params.ClientIP,
						"status_code": params.StatusCode,
						"time":        params.TimeStamp.Format(time.RFC3339),
					})

					return string(s) + "\n"
				}

				return ""
			},

			Output:    gin.DefaultWriter,
			SkipPaths: []string{"/health"},
		},
	))

	router.HandleMethodNotAllowed = true

	router.GET("/metadata/datasets", SelectedMiddleware(), sda.Datasets)
	router.GET("/metadata/datasets/*dataset", SelectedMiddleware(), sda.Files)
	router.GET("/files/:fileid", SelectedMiddleware(), sda.Download)
	router.GET("/s3/*path", SelectedMiddleware(), s3.Download)
	router.HEAD("/s3/*path", SelectedMiddleware(), s3.Download)
	router.GET("/health", healthResponse)
	router.HEAD("/", healthResponse)

	// Configure TLS settings
	log.Info("(3/5) Configuring TLS")
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	// Configure web server
	log.Info("(4/5) Configuring server")
	srv := &http.Server{
		Addr:              config.Config.App.Host + ":" + fmt.Sprint(config.Config.App.Port),
		Handler:           router,
		TLSConfig:         cfg,
		TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      -1,
	}

	return srv
}
