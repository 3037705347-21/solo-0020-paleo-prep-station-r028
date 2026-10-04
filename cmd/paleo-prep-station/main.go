package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

func main() {
	os.Exit(run())
}

func run() int {
	addr := flag.String("addr", "127.0.0.1:18133", "local HTTP listen address")
	var overrides []station.OperatorAssignment
	flag.Func("operator", "登记或变更操作人职责（格式 name:role，可重复）", func(value string) error {
		name, roleValue, ok := strings.Cut(value, ":")
		if !ok {
			return fmt.Errorf("invalid operator %q, want name:role", value)
		}
		role, err := domain.ParseRole(roleValue)
		if err != nil {
			return err
		}
		overrides = append(overrides, station.OperatorAssignment{
			Name: strings.TrimSpace(name),
			Role: role,
		})
		return nil
	})
	flag.Parse()

	repo := repository.NewMemory()
	service := station.New(repo, station.WithOperators(station.DefaultOperators()...))
	for _, override := range overrides {
		if err := service.AssignRole(context.Background(), override.Name, override.Role); err != nil {
			fmt.Fprintln(os.Stderr, "operator setup failed:", err)
			return 1
		}
	}
	server := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.NewRouter(service),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	log.Printf("paleo prep station listening on %s", *addr)
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "server stopped:", err)
		return 1
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(os.Stderr, "server shutdown:", err)
			return 1
		}
		return 0
	}
}
