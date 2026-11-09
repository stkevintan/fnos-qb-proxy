package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v2"
)

func proxyCmd(ctx *cli.Context) error {
	debug := ctx.Bool("debug")
	port := ctx.Int("port")
	expectedPassword := ctx.String("password")
	proxy := NewFnosProxy(debug, expectedPassword, port)
	credentialSocket := os.Getenv("FNOS_QB_CREDENTIAL_SOCKET")
	if credentialSocket == "" {
		credentialSocket = defaultCredentialSocket
	}
	if err := os.MkdirAll(filepath.Dir(credentialSocket), 0755); err != nil {
		return fmt.Errorf("create credential socket directory: %w", err)
	}
	go func() {
		if err := proxy.credentials.serve(credentialSocket); err != nil {
			log.Printf("credential socket stopped: %v", err)
			os.Exit(1)
		}
	}()
	fmt.Printf("proxy running on port %d\n", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), proxy)
	if err != nil {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func main() {
	if isWrapperInvocation() {
		runWrapper()
		return
	}

	if err := installWrapper(); err != nil {
		log.Printf("warning: could not install qBittorrent wrapper: %v", err)
	}

	app := &cli.App{
		Name:   "fnos-qb-proxy",
		Usage:  "fnos-qb-proxy is a proxy for qBittorrent in fnOS",
		Action: proxyCmd,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "password",
				Aliases: []string{"p"},
				Usage:   "if not set, qBittorrent will login automatically",
				Value:   "",
			},
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Value:   false,
			},
			&cli.IntFlag{
				Name:  "port",
				Usage: "proxy running port",
				Value: 8080,
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
