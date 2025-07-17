package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	_ "github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
	"github.com/urfave/cli/v2"
	"github.com/wayneashleyberry/gh-arc/pkg/cmd"
)

func setDefaultLogger(level slog.Leveler) {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	logger := slog.New(handler)

	slog.SetDefault(logger)
}

func main() {
	ctx := context.Background()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(_ context.Context) error {
	setDefaultLogger(slog.LevelInfo)

	app := &cli.App{
		Name:  "arc",
		Usage: "List archived dependencies",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "debug",
				Value: false,
				Usage: "Print debug logs",
				Action: func(_ *cli.Context, v bool) error {
					if v {
						setDefaultLogger(slog.LevelDebug)
					}

					return nil
				},
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "gomod",
				Usage: "List archived go modules",
				Action: func(_ *cli.Context) error {
					return cmd.ListArchivedGoModules()
				},
			},
		},
	}

	return app.Run(os.Args)
}
