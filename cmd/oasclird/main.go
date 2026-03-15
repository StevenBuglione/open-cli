package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
	"github.com/StevenBuglione/oas-cli-go/pkg/instance"
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
	server := runtime.NewServer(runtime.Options{
		AuditPath:            *auditPath,
		CacheDir:             *cacheDir,
		StateDir:             paths.StateDir,
		DefaultConfigPath:    *configPath,
		InstanceID:           paths.InstanceID,
		RuntimeURL:           runtimeURL,
		HeartbeatSeconds:     *heartbeatSeconds,
		MissedHeartbeatLimit: *missedHeartbeatLimit,
		ShutdownMode:         *shutdownMode,
		SessionScope:         *sessionScope,
		ShareMode:            *shareMode,
		ConfigFingerprint:    *configFingerprint,
		Shutdown:             listener.Close,
	})
	info := instance.RuntimeInfo{
		InstanceID: paths.InstanceID,
		URL:        runtimeURL,
		AuditPath:  *auditPath,
		CacheDir:   *cacheDir,
	}
	if err := instance.WriteRuntimeInfo(paths.RuntimePath, info); err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.Serve(listener, server.Handler()))
}
