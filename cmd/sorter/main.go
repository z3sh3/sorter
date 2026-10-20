package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"sorter/internal/config"
	"sorter/internal/server"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Subcommand: hash-token generates the SHA-256 hash of an access token
	// for use in config.yaml (auth.token_hash).
	if len(os.Args) >= 2 && os.Args[1] == "hash-token" {
		runHashToken(os.Args[2:])
		return
	}

	fs := flag.NewFlagSet("sorter", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	_ = fs.Parse(os.Args[1:])

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	if err := srv.Run(); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func runHashToken(args []string) {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: sorter hash-token <token>")
		os.Exit(2)
	}
	sum := sha256.Sum256([]byte(args[0]))
	fmt.Println("sha256:" + hex.EncodeToString(sum[:]))
}
