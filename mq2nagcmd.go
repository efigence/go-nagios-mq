package main

import (
	"os"

	"encoding/json"
	"fmt"
	"github.com/efigence/go-nagios"
	"github.com/op/go-logging"
	"github.com/urfave/cli"
	"github.com/zerosvc/go-zerosvc"
)

var version string
var log = logging.MustGetLogger("main")
var stdout_log_format = logging.MustStringFormatter("%{color:bold}%{time:2006-01-02T15:04:05.0000Z-07:00}%{color:reset}%{color} [%{level:.1s}] %{color:reset}%{shortpkg}[%{longfunc}] %{message}")
var end chan bool

func main() {
	stderrBackend := logging.NewLogBackend(os.Stderr, "", 0)
	stderrFormatter := logging.NewBackendFormatter(stderrBackend, stdout_log_format)
	logging.SetBackend(stderrFormatter)
	logging.SetFormatter(stdout_log_format)

	log.Infof("Starting app version: %s", version)
	app := cli.NewApp()
	app.Name = "mq2nagcmd"
	app.Description = "Receive passive check info from MQ and pipe it to Icinga/Nagios or any compatible monitoring system"
	app.Version = version
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "amqp-url",
			Value:  "amqp://guest:guest@localhost:5672/",
			Usage:  "AMQP url-flag",
			EnvVar: "AMQP_URL",
		},
		cli.StringFlag{
			Name:   "cmd-file",
			Value:  "/tmp/nagios-cmd-test",
			Usage:  "path to Nagios/Icinga CMD file",
			EnvVar: "CMD_FILE_PATH",
		},
		cli.StringFlag{
			Name:  "exchange",
			Value: "monitoring",
			Usage: "exchange to receive events on",
		},
		cli.StringFlag{
			Name:  "topic",
			Value: "check.results.#",
			Usage: "topic to subscribe to",
		},
		cli.BoolFlag{
			Name:  "shared-queue,shared",
			Usage: "Whether queue should be shared between instance. Also switches it to persistent mode",
		},
	}
	app.Action = func(c *cli.Context) error {
		log.Error("running")
		MainLoop(c)
		return nil
	}
	app.Run(os.Args)
	_ = <-end
}

func MainLoop(c *cli.Context) error {
	hostname, _ := os.Hostname()
	node, err := zerosvc.New("nagcmd-receiver@"+hostname,
		zerosvc.TransportAMQP(
			c.GlobalString("amqp-url"),
			zerosvc.TransportAMQPConfig{
				SharedQueue: c.GlobalBool("shared-queue"),
				QueueTTL:    1000 * 86400,
			},
		),
	)
	if err != nil {
		log.Errorf("can't connect to queue: %s")
	}
	events, err := node.GetEventsCh(c.GlobalString("topic"))
	go func() {
		for ev := range events {
			if cmd, ok := ev.Headers["command"]; ok {
				switch cmd {
				case nagios.CmdProcessHostCheckResult:
					host := nagios.NewHost()
					err := json.Unmarshal(ev.Body, &host)
					if err != nil {
						log.Warningf("Error when decoding host check: %s", err)
						continue
					}
					fmt.Println(nagios.EncodeHostCheck(host))

				case nagios.CmdProcessServiceCheckResult:
					service := nagios.NewService()
					err := json.Unmarshal(ev.Body, &service)
					if err != nil {
						log.Warningf("Error when decoding host check: %s", err)
						continue
					}
					fmt.Println(nagios.EncodeServiceCheck(service))
				default:
					log.Warningf("Cmd not supported: %s", cmd)
				}
			}
		}
	}()
	return nil
}
