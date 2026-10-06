package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zigamedved/cronsentry/internal/api"
	"github.com/zigamedved/cronsentry/internal/db"
	"github.com/zigamedved/cronsentry/internal/notifications"
	"github.com/zigamedved/cronsentry/internal/notifications/integrations"
)

func main() {
	logger := log.New(os.Stdout, "cronsentry: ", log.LstdFlags)

	database, err := db.NewDatabase()
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	if err := database.InitDatabase(); err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	logger.Println("Database initialized successfully")

	apiKey := os.Getenv("SENDGRID_API_KEY")
	enabled := apiKey != ""
	if !enabled {
		apiKey = "disabled"
		logger.Println("SENDGRID_API_KEY unset; email notifications run in dry-run mode")
	}
	sendgridClient := integrations.NewSendgridSendClient(apiKey, logger, enabled)
	notificationProcessor := notifications.NewNotificationProcessor(
		database.GetDB(),
		sendgridClient,
		logger,
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
