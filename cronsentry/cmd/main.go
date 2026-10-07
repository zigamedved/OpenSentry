package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zigamedved/OpenSentry/internal/api"
	"github.com/zigamedved/OpenSentry/internal/db"
	"github.com/zigamedved/OpenSentry/internal/notifications"
	"github.com/zigamedved/OpenSentry/internal/notifications/integrations"
)

func main() {
	logger := log.New(os.Stdout, "opensentry: ", log.LstdFlags)

	database, err := db.NewDatabase()
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	if err := database.InitDatabase(); err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	logger.Println("Database initialized successfully")

	if err := database.SyncEnvAlertChannels("test-user", os.Getenv("SLACK_WEBHOOK_URL"), os.Getenv("DISCORD_WEBHOOK_URL")); err != nil {
		logger.Fatalf("Failed to store alert channel env: %v", err)
	}

	apiKey := os.Getenv("SENDGRID_API_KEY")
	enabled := apiKey != ""
	if !enabled {
		apiKey = "disabled"
		logger.Println("SENDGRID_API_KEY unset; email notifications run in dry-run mode")
	}
	fromName, fromEmail := integrations.ParseEmailFrom(os.Getenv("EMAIL_FROM"))
	sendgridClient := integrations.NewSendgridSendClient(apiKey, logger, enabled, fromName, fromEmail)
	notificationProcessor := notifications.NewNotificationProcessor(
		database.GetDB(),
		sendgridClient,
		logger,
		os.Getenv("DASHBOARD_URL"),
	)
	notificationProcessor.Start()
	logger.Println("Notification processor started")

	apiToken := os.Getenv("API_TOKEN")
	if apiToken == "" {
		logger.Println("API_TOKEN unset; management routes will return 401")
	}
	server := api.NewServer(database, logger, apiToken)
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	srv := &http.Server{
		Addr:         addr,
		Handler:      server.Router(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  2 * time.Minute,
	}

	go func() {
		logger.Printf("Starting server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Failed to start server: %v", err)
		}
	}()

	jobChecker := db.NewJobChecker(database, logger)
	jobChecker.Start()
	logger.Println("Job checker started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Println("Shutting down server...")

	jobChecker.Stop()
	logger.Println("Job checker stopped")

	notificationProcessor.Stop()
	logger.Println("Notification processor stopped")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatalf("Server forced to shutdown: %v", err)
	}

	logger.Println("Server exited properly")
}
