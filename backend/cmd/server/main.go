package main

import (
	"log"
	"net/http"
	"os"

	"github.com/ccmux/backend/internal/api"
	"github.com/ccmux/backend/internal/hub"
	"github.com/ccmux/backend/internal/notify"
	"github.com/ccmux/backend/internal/store"
	"github.com/ccmux/backend/migrations"
	"github.com/redis/go-redis/v9"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		runMigrate()
		return
	}
	runServer()
}

func runMigrate() {
	db, err := store.Open(mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(migrations.FS); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations applied successfully")
}

func runServer() {
	db, err := store.Open(mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	// Auto-migrate on startup (idempotent IF NOT EXISTS migrations).
	if err := db.Migrate(migrations.FS); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	redisOpts, err := redis.ParseURL(mustEnv("REDIS_URL"))
	if err != nil {
		log.Fatalf("redis url: %v", err)
	}
	rdb := redis.NewClient(redisOpts)

	multiInstance := os.Getenv("MULTI_INSTANCE_MODE") == "true"
	h := hub.New(rdb, multiInstance)

	fcm := notify.NewFCMSender(
		os.Getenv("FCM_SERVICE_ACCOUNT_PATH"),
		os.Getenv("FCM_PROJECT_ID"),
	)

	app := &api.App{
		DB:         db,
		Hub:        h,
		Notify:     notify.NewDispatcher(db, fcm),
		JWTSecret:  mustEnv("JWT_SECRET"),
		HMACSecret: mustEnv("HMAC_SECRET"),
	}

	addr := envOr("SERVER_ADDR", ":8080")
	log.Printf("ccmux backend listening on %s (multi_instance=%v)", addr, multiInstance)
	if err := http.ListenAndServe(addr, app.NewRouter()); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
