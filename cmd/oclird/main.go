package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/instance"
)

func main() {
	addr := flag.String("addr", "", "listen address (defaults to an automatic local port)")
	configPath := flag.String("config", "", "default .cli.json path used for instance derivation and requests")
	instanceID := flag.String("instance-id", "", "instance id for isolated runtime state")
	stateDir := flag.String("state-dir", "", "state directory root for runtime metadata and audit logs")
	auditPath := flag.String("audit-path", "", "audit log path")
	cacheDir := flag.String("cache-dir", "", "cache directory for remote discovery and OpenAPI documents")
	heartbeatSeconds := flag.Int("heartbeat-seconds", 0, "heartbeat interval in seconds for lease management")
	missedHeartbeatLimit := flag.Int("missed-heartbeat-limit", 0, "number of missed heartbeats before session expiry")
	shutdownMode := flag.String("shutdown", "", "shutdown mode: when-owner-exits or manual")
	sessionScope := flag.String("session-scope", "", "local runtime session scope")
	shareMode := flag.String("share", "", "local runtime share mode")
	shareKeyPresent := flag.Bool("share-key-present", false, "whether a shared-group runtime was derived from a configured share key")
	configFingerprint := flag.String("config-fingerprint", "", "local runtime config fingerprint")
	flag.Parse()

	paths, err := instance.Resolve(instance.Options{
		InstanceID: *instanceID,
		ConfigPath: *configPath,
		StateRoot:  *stateDir,
	})
	if err != nil {
		log.Fatal(err)
	}
	if *auditPath == "" {
		*auditPath = paths.AuditPath
	}
	if *cacheDir == "" {
		*cacheDir = paths.CacheDir
	}
	listenAddr := *addr
	if listenAddr == "" {
		listenAddr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal(err)
	}
	runtimeURL := "http://" + listener.Addr().String()
	var httpServer *http.Server
	server := runtime.NewServer(runtime.Options{
		AuditPath:            *auditPath,
		CacheDir:             *cacheDir,
		StateDir:             paths.StateDir,
		DefaultConfigPath:    *configPath,
		InstanceID:           paths.InstanceID,
		RuntimeURL:           runtimeURL,
		RuntimeMode:          "local",
		HeartbeatSeconds:     *heartbeatSeconds,
		MissedHeartbeatLimit: *missedHeartbeatLimit,
		ShutdownMode:         *shutdownMode,
		SessionScope:         *sessionScope,
		ShareMode:            *shareMode,
		ShareKeyPresent:      *shareKeyPresent,
		ConfigFingerprint:    *configFingerprint,
		Shutdown: func() error {
			if httpServer != nil {
				return httpServer.Close()
			}
			return listener.Close()
		},
	})
	info := instance.RuntimeInfo{
		InstanceID: paths.InstanceID,
		URL:        runtimeURL,
		PID:        os.Getpid(),
		AuditPath:  *auditPath,
		CacheDir:   *cacheDir,
	}
	if err := instance.WriteRuntimeInfo(paths.RuntimePath, info); err != nil {
		log.Fatal(err)
	}
	httpServer = &http.Server{Handler: server.Handler()}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	var shutdownOnce sync.Once
	shutdown := func(trigger string) {
		shutdownOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if httpServer != nil {
				_ = httpServer.Close()
			}
			_ = server.CloseWithContext(ctx)
			_ = os.Remove(paths.RuntimePath)
			log.Printf("runtime shutdown: %s", trigger)
		})
	}

	go func() {
		sig := <-signalCh
		shutdown(sig.String())
	}()

	err = httpServer.Serve(listener)
	shutdown("serve_exit")
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
