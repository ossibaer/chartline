package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

//go:embed web
var webFiles embed.FS

func main() {
	loadEnv(".env")
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	app, err := newApp(env("DATA_DIR", "data"), assets)
	if err != nil {
		log.Fatal(err)
	}
	app.accessKey = os.Getenv("ACCESS_KEY")
	app.discord = discordConfig{ClientID: os.Getenv("DISCORD_CLIENT_ID"), Secret: os.Getenv("DISCORD_CLIENT_SECRET"), Allowed: strings.Split(os.Getenv("DISCORD_ALLOWED_USER_IDS"), ",")}
	app.publicOrigin = os.Getenv("PUBLIC_ORIGIN")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go app.runClock(ctx)
	server := &http.Server{Addr: env("ADDR", "127.0.0.1:8080"), Handler: app.handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		app.closeConnections()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Chartline is ready at http://%s", server.Addr)
	log.Printf("Loaded %d songs from %s/audio. Rooms reset when the server restarts.", len(app.library), app.dataDir)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, strings.Trim(strings.TrimSpace(value), "\"'"))
		}
	}
}
