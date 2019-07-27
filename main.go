package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"

	"github.com/udovin/solve/api"
	"github.com/udovin/solve/config"
	"github.com/udovin/solve/core"
)

// Path to unix '/etc' directory
const etcDir = "/etc/solve"

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func getConfig() (config.Config, error) {
	path, ok := os.LookupEnv("SOLVE_CONFIG_FILE")
	if ok {
		return config.LoadFromFile(path)
	}
	path = "config.json"
	if fileExists(path) {
		return config.LoadFromFile(path)
	}
	path = filepath.Join(etcDir, path)
	if fileExists(path) {
		return config.LoadFromFile(path)
	}
	return config.Config{}, errors.New("unable to find config file")
}

func getAddress(cfg config.ServerConfig) string {
	return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
}

func main() {
	cfg, err := getConfig()
	if err != nil {
		panic(err)
	}
	app, err := core.NewApp(&cfg)
	if err != nil {
		panic(err)
	}
	if err := app.Start(); err != nil {
		panic(err)
	}
	defer app.Stop()
	server := echo.New()
	server.Use(middleware.Recover())
	server.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${time_rfc3339}\t${latency_human}\t${status}\t${method}\t${uri}\n",
	}))
	server.Pre(middleware.RemoveTrailingSlash())
	server.Use(middleware.Gzip())
	api.Register(app, server)
	server.Logger.Fatal(server.Start(fmt.Sprintf(
		"%s:%d", cfg.Server.Host, cfg.Server.Port,
	)))
}
