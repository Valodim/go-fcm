package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Valodim/go-fcm"
	"github.com/urfave/cli"
)

func main() {
	log.SetFlags(0)

	app := cli.NewApp()
	app.Name = "go-fcm"
	app.Version = "1.0.0"
	app.Compiled = time.Now()
	app.UsageText = "go-fcm [global options]"
	app.Usage = "Send messages to devices through Firebase Cloud Messaging."

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "topic, t",
			Usage: "The name of the topic to send a message to.",
		},
		cli.StringSliceFlag{
			Name:  "tokens, k",
			Usage: "The device topic or registration ids to send messages to.",
		},
		cli.StringFlag{
			Name:  "condition, c",
			Usage: "The condition to send a message to, e.g. 'foo' in topics && 'bar' in topics",
		},
		cli.StringFlag{
			Name:  "title",
			Usage: "The notification title.",
		},
		cli.StringFlag{
			Name:  "body",
			Usage: "The notification body.",
		},
		cli.BoolFlag{
			Name:  "validate-only",
			Usage: "Validate the message, but don't send it.",
		},
		cli.StringFlag{
			Name:   "credentials-location",
			EnvVar: "CREDENTIALS_LOCATION",
			Usage:  "Location of the Firebase Admin SDK JSON credentials.",
			Value:  "fcm-credentials.json",
		},
		cli.StringFlag{
			Name:   "project-id",
			EnvVar: "PROJECT_ID",
			Usage:  "The id of your Firebase project.",
		},
	}

	app.Action = func(c *cli.Context) error {
		err := setupNotification(c)
		if err != nil {
			log.Fatal(err.Error())
		}
		return err
	}

	app.Run(os.Args)
}

func setupNotification(c *cli.Context) error {
	topic := c.String("topic")
	tokens := c.StringSlice("tokens")
	condition := c.String("condition")
	title := c.String("title")
	body := c.String("body")
	validateOnly := c.Bool("validate-only")
	credentialsLocation := c.String("credentials-location")
	projectID := c.String("project-id")

	message := &fcm.MulticastMessage{
		Tokens: tokens,
		Message: &fcm.Message{
			Topic:     topic,
			Condition: condition,
			Notification: &fcm.Notification{
				Title: title,
				Body:  body,
			},
		},
	}

	client, err := fcm.NewClient(projectID, credentialsLocation)
	if err != nil {
		return err
	}

	var result *fcm.MulticastResponse
	if validateOnly {
		result, err = client.SendMulticast(context.Background(), message)
	} else {
		result, err = client.SendMulticastDryRun(context.Background(), message)
	}
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(result, " ", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(out))
	return nil
}
