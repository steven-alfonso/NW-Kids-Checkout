package cmd

import (
	"time"

	"kids-checkin/internal/cmd/apiserver"
	"kids-checkin/internal/cmd/checkins"
	"kids-checkin/internal/cmd/checkoutsfetcher"
	"kids-checkin/internal/cmd/location"
	"kids-checkin/internal/db"

	"github.com/urfave/cli/v3"
)

func NewCommand() *cli.Command {
	return &cli.Command{
		Commands: []*cli.Command{
			{
				Name:  "apiserver",
				Usage: "Starts the API server",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "port",
						Value:   3000,
						Sources: cli.NewValueSourceChain(cli.EnvVar("PORT")),
					},
					&cli.StringFlag{
						Name:    "db-file",
						Value:   db.DefaultDBFile,
						Sources: cli.NewValueSourceChain(cli.EnvVar("DB_FILE")),
					},
				},
				Action: apiserver.ServeCmd,
			},
			{
				Name:  "checkout-fetcher",
				Usage: "Fetches checkouts from Planning Center",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "db-file",
						Value:   db.DefaultDBFile,
						Sources: cli.NewValueSourceChain(cli.EnvVar("DB_FILE")),
					},
					&cli.DurationFlag{
						Name:    "interval",
						Value:   3 * time.Second,
						Sources: cli.NewValueSourceChain(cli.EnvVar("FETCH_CHECKOUTS_INTERVAL")),
					},
					&cli.DurationFlag{
						Name:    "event-update-interval",
						Value:   3 * time.Second,
						Sources: cli.NewValueSourceChain(cli.EnvVar("EVENT_UPDATE_INTERVAL")),
					},
					&cli.DurationFlag{
						Name:  "runtime",
						Usage: "How long to run the fetcher for",
						Value: 5000 * time.Second,
					},
					&cli.BoolFlag{
						Name:    "service",
						Usage:   "Run continuously as a service, ignoring --runtime",
						Sources: cli.NewValueSourceChain(cli.EnvVar("FETCH_CHECKOUTS_SERVICE")),
					},
					&cli.BoolFlag{
						Name:    "use-check-windows",
						Usage:   "Only fetch checkouts for events whose current time falls within a configured check window",
						Sources: cli.NewValueSourceChain(cli.EnvVar("FETCH_CHECKOUTS_USE_CHECK_WINDOWS")),
					},
				},
				Action: checkoutsfetcher.FetchCheckouts,
			},
			{
				Name:     "locations",
				Usage:    "Commands to manage locations",
				Commands: location.Commands,
			},
			{
				Name:     "checkins",
				Usage:    "Commands to manage checkins",
				Commands: checkins.Commands,
			},
		},
	}
}
