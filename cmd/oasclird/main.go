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
	server := runtime.NewServer(runtime.Options{
		AuditPath:         *auditPath,
		CacheDir:          *cacheDir,
		DefaultConfigPath: *configPath,
	})
	listenAddr := *addr
	if listenAddr == "" {
		listenAddr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal(err)
	}
	info := instance.RuntimeInfo{
		InstanceID: paths.InstanceID,
		URL:        "http://" + listener.Addr().String(),
		AuditPath:  *auditPath,
		CacheDir:   *cacheDir,
	}
	if err := instance.WriteRuntimeInfo(paths.RuntimePath, info); err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.Serve(listener, server.Handler()))
}
