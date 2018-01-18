package main

import (
	"os"

	"encoding/json"
	"github.com/efigence/go-nagios"
	"github.com/op/go-logging"
	"github.com/urfave/cli"
	"github.com/zerosvc/go-zerosvc"
	"time"
)

var version string
var log = logging.MustGetLogger("main")
var stdout_log_format = logging.MustStringFormatter("%{color:bold}%{time:2006-01-02T15:04:05.0000Z-07:00}%{color:reset}%{color} [%{level:.1s}] %{color:reset}%{shortpkg}[%{longfunc}] %{message}")
var end chan bool

var selfcheck = nagios.NewService()

func main() {
	stderrBackend := logging.NewLogBackend(os.Stderr, "", 0)
	stderrFormatter := logging.NewBackendFormatter(stderrBackend, stdout_log_format)
	logging.SetBackend(stderrFormatter)
	logging.SetFormatter(stdout_log_format)
	hostname, _ := os.Hostname()

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
		cli.BoolFlag{
			Name:  "disable-selfcheck",
			Usage: "By default, selfcheck event (with current hostname) will be generated every minute. This flag disables that",
		},
		cli.StringFlag{
			Name:  "selfcheck-host",
			Usage: "Self-check hostname",
			Value: hostname,
		},
		cli.StringFlag{
			Name:  "selfcheck-service",
			Usage: "Self-check service name",
			Value: "mq2nagcmd",
		},
	}
	app.Action = func(c *cli.Context) error {
		selfcheck.Hostname = c.String("selfcheck-host")
		selfcheck.Description = c.String("selfcheck-service")
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
	cmdPipe, err := nagios.NewCmd(c.GlobalString("cmd-file"))
	if err != nil {
		log.Errorf("can't open nagios cmd file [%s], %s", c.GlobalString("cmd-file"), err)
		end <- true
		return nil
	}
	selfcheck.UpdateStatus(nagios.StateOk, "Running")
	if !c.Bool("disable-selfcheck") {
		log.Notice("Generating selfcheck event every minute")
		go RunSelfcheck(cmdPipe, &selfcheck)
	}
	log.Notice("Connected to MQ and cmd file, entering main loop")
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
					cmdPipe.Send(nagios.CmdProcessHostCheckResult, nagios.EncodeHostCheck(host))

				case nagios.CmdProcessServiceCheckResult:
					service := nagios.NewService()
					err := json.Unmarshal(ev.Body, &service)
					if err != nil {
						log.Warningf("Error when decoding host check: %s", err)
						continue
					}

				default:
					log.Warningf("Cmd not supported: %s", cmd)
				}
			}
		}
	}()
	return nil
}

func RunSelfcheck(cmdPipe *nagios.Command, check *nagios.Service) {
	for {
		cmdPipe.Send(nagios.CmdProcessServiceCheckResult, nagios.EncodeServiceCheck(*check))
		time.Sleep(time.Minute)
	}
}
