// gzipserver is a disposable profiling fixture, never a production server.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"receipt/internal/cache"
	"receipt/internal/fingerprint"
	"runtime"
	"syscall"
	"time"
)

type config struct {
	Address     string `json:"address"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
	Cache       string `json:"cache"`
	Key         string `json:"key"`
	Token       string `json:"token"`
	Gzip        bool   `json:"gzip"`
}

type resources struct {
	CPUUS  int64 `json:"cpu_us"`
	MaxRSS int64 `json:"max_rss_native"`
}

func usage() (resources, error) {
	var value syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &value); err != nil {
		return resources{}, err
	}
	return resources{value.Utime.Sec*1000000 + int64(value.Utime.Usec) + value.Stime.Sec*1000000 + int64(value.Stime.Usec), value.Maxrss}, nil
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gzip fixture failed")
		os.Exit(1)
	}
}

func execute() error {
	if len(os.Args) == 3 && os.Args[1] == "certificate" {
		return certificate(os.Args[2])
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("missing fixture configuration")
	}
	var cfg config
	if err := json.Unmarshal(scanner.Bytes(), &cfg); err != nil {
		return err
	}
	pair, err := tls.LoadX509KeyPair(cfg.Certificate, cfg.PrivateKey)
	if err != nil {
		return err
	}
	store := cache.Disk{Dir: cfg.Cache}
	saved, err := store.Get(context.Background(), cfg.Key)
	if err != nil {
		return err
	}
	encoded, err := cache.Encode(saved)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return err
	}
	defer listener.Close() //nolint:errcheck // Server shutdown owns the listener; fallback cleanup only.
	handler := compression(cache.Handler(store, cfg.Token), cfg.Gzip, os.Stderr)
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}},
	}
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, "", "") }()
	output := json.NewEncoder(os.Stdout)
	if err := output.Encode(struct {
		PID    int    `json:"pid"`
		Bytes  int    `json:"decoded_bytes"`
		SHA256 string `json:"decoded_sha256"`
		Go     string `json:"go_version"`
	}{os.Getpid(), len(encoded), fingerprint.Hash(encoded), runtime.Version()}); err != nil {
		return err
	}
	for scanner.Scan() {
		switch scanner.Text() {
		case "snapshot":
			value, err := usage()
			if err != nil {
				return err
			}
			if err := output.Encode(value); err != nil {
				return err
			}
		case "stop":
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := server.Shutdown(ctx)
			cancel()
			serveErr := <-done
			if errors.Is(serveErr, http.ErrServerClosed) {
				return err
			}
			return errors.Join(err, serveErr)
		default:
			return fmt.Errorf("unknown fixture command")
		}
	}
	return errors.Join(scanner.Err(), server.Close())
}
