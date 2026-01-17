package main

import (
	"os"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/containrrr/shoutrrr/pkg/types"
)

// url := "slack://token-a/token-b/token-c"
var url string
var sender *router.ServiceRouter

func initAlerting() error {
	url := os.Getenv("SHOUTRRR_URL")

	var err error
	sender, err = shoutrrr.CreateSender(url)
	return err
}

func sendAlert(title string, message string) error {
	err := sender.Send(message, &types.Params{"title": title})
	if len(err) > 0 {
		return err[0]
	}
	return nil
}
