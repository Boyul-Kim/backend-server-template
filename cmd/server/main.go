package main

import (
	"log"
	"os"

	"github.com/boyulkim/api-template/internal/app"
	"github.com/boyulkim/api-template/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration:\n%v", err)
	}
	for _, warning := range cfg.Warnings {
		log.Printf("WARN: %s", warning)
	}

	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := app.Healthcheck(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	pb := app.New(cfg)
	pb.RootCmd.SetArgs(app.ServeArgs(os.Args[1:], cfg))

	if err := pb.Start(); err != nil {
		log.Fatal(err)
	}
}
