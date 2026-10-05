package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

//go:embed schema_embed.sql
var schemaSQL string

func Connect(ctx context.Context, dsn string) (*sql.DB, error) {
	pool, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)

	for i := 0; i < 10; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.PingContext(pingCtx)
		cancel()
		if err == nil {
			break
		}
		log.Printf("[db] waiting for postgres (attempt %d/10): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	if _, err := pool.ExecContext(ctx, schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	log.Println("[db] connected and schema applied")
	return pool, nil
}
