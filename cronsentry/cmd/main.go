package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	slackURL := os.Getenv("SLACK_WEBHOOK_URL")
	discordURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if demoSeedEnabled() {
		userID, err := database.SeedDemoUser()
		if err != nil {
			logger.Fatalf("Failed to seed demo user: %v", err)
		}
		logger.Printf("Demo account ready: test@example.com (user %s)", userID)
		if err := database.SyncEnvAlertChannels(userID, slackURL, discordURL); err != nil {
			logger.Fatalf("Failed to store alert channel env: %v", err)
		}
	} else if slackURL != "" || discordURL != "" {
		logger.Println("SLACK_WEBHOOK_URL and DISCORD_WEBHOOK_URL apply only when DEMO_SEED is set; configure channels in the dashboard")
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

	server := api.NewServer(database, logger)
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

func demoSeedEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DEMO_SEED"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
