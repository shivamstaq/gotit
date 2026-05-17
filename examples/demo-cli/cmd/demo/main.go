// Demo is a tiny CLI used to exercise the gotit runner in examples/demo-suite.
// Subcommands:
//
//	demo version           — print version+JSON, exit 0.
//	demo add A B           — print sum, exit 0.
//	demo crash             — print message to stderr, exit 1.
//	demo greet --name=foo  — print greeting, exit 0.
//	demo serve [flags]     — HTTP echo server, handles SIGTERM cleanly.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: demo <version|add|crash|greet>")
	}
	switch args[0] {
	case "version":
		out := map[string]any{"name": "demo", "version": version}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return nil
	case "add":
		if len(args) != 3 {
			return errors.New("usage: demo add A B")
		}
		a, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("A: %w", err)
		}
		b, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("B: %w", err)
		}
		fmt.Println(a + b)
		return nil
	case "crash":
		return errors.New("intentional crash")
	case "greet":
		fs := flag.NewFlagSet("greet", flag.ContinueOnError)
		name := fs.String("name", "world", "name to greet")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		fmt.Printf("Hello, %s!\n", *name)
		return nil
	case "serve":
		return runServe(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// runServe starts an HTTP echo server. Flags:
//
//	--port-file <path>      write the listening port (or 0 for auto) to this file
//	--ready-marker <s>      print this string to stdout once listening (default "listening")
//	--ignore-sigterm        intentionally ignore SIGTERM (for shutdown-grace tests)
//	--crash-after <dur>     exit non-zero after this duration (for mid-spec death tests)
//	--port <n>              listen on this port (default 0 = auto-assigned)
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	portFile := fs.String("port-file", "", "write the listening port to this file")
	readyMarker := fs.String("ready-marker", "listening", "stdout marker printed once listening")
	ignoreSigterm := fs.Bool("ignore-sigterm", false, "ignore SIGTERM (for grace-timeout tests)")
	crashAfter := fs.Duration("crash-after", 0, "exit non-zero after this duration (0 disables)")
	port := fs.Int("port", 0, "listening port (0 = auto)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	if *portFile != "" {
		if err := os.MkdirAll(filepath.Dir(*portFile), 0o755); err != nil {
			return fmt.Errorf("create port-file dir: %w", err)
		}
		if err := os.WriteFile(*portFile, []byte(strconv.Itoa(actualPort)), 0o644); err != nil {
			return fmt.Errorf("write port-file: %w", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, r.URL.Query().Get("msg"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{Handler: mux}
	fmt.Printf("%s on port %d\n", *readyMarker, actualPort)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	sigCh := make(chan os.Signal, 1)
	if *ignoreSigterm {
		// Install a no-op handler so SIGTERM does not take the default
		// terminate action. This is the "daemon that refuses to die
		// gracefully" scenario gotit verifies via the stop.grace timeout.
		ignoreCh := make(chan os.Signal, 1)
		signal.Notify(ignoreCh, syscall.SIGTERM)
		go func() {
			for range ignoreCh {
				fmt.Fprintln(os.Stderr, "ignoring SIGTERM")
			}
		}()
	} else {
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	}

	var crashCh <-chan time.Time
	if *crashAfter > 0 {
		crashCh = time.After(*crashAfter)
	}

	select {
	case sig := <-sigCh:
		fmt.Printf("received %s, shutting down\n", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		fmt.Println("graceful shutdown complete")
		return nil
	case <-crashCh:
		fmt.Fprintln(os.Stderr, "intentional crash after timer")
		os.Exit(2)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
	return nil
}
