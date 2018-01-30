package main

import (
	"os"

	"encoding/json"
	"fmt"
	"github.com/efigence/go-nagios"
	"github.com/efigence/go-nagios-mq/utils"
	"github.com/op/go-logging"
	"github.com/urfave/cli"
	"github.com/zerosvc/go-zerosvc"
	"strings"
	"time"
)

var version string
var log = logging.MustGetLogger("main")
var stdout_log_format = logging.MustStringFormatter("%{color:bold}%{time:2006-01-02T15:04:05.0000Z-07:00}%{color:reset}%{color} [%{level:.1s}] %{color:reset}%{shortpkg}[%{longfunc}] %{message}")
var end = make(chan bool)
var debug = false

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
			Name:  "topic-prefix",
			Value: "check.results",
			Usage: "topic prefix",
		},
		cli.BoolFlag{
			Name:  "shared-queue,shared",
			Usage: "Whether queue should be shared between instance. Also switches it to persistent mode",
		},
		cli.BoolFlag{
			Name:  "disable-selfcheck",
			Usage: "By default, selfcheck event (with current hostname) will be generated every minute. This flag disables that",
		},
		cli.BoolFlag{
			Name:  "strip-fqdn",
			Usage: "remove part of hostname after dot",
		},
		cli.BoolFlag{
			Name:  "debug",
			Usage: "Debug",
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
		debug = c.Bool("debug")
		return MainLoop(c)
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Errorf("exiting: %s", err)
		os.Exit(1)

	}

}

func MainLoop(c *cli.Context) error {
	hostname, _ := os.Hostname()
	node, err := zerosvc.New("nagcmd-receiver@"+hostname,
		zerosvc.TransportAMQP(
			c.GlobalString("amqp-url"),
			zerosvc.TransportAMQPConfig{
				SharedQueue:   c.GlobalBool("shared-queue"),
				QueueTTL:      1000 * 86400,
				EventExchange: c.GlobalString("exchange"),
			},
		),
	)
	if err != nil {
		log.Errorf("can't connect to queue: %s", err)
		os.Exit(1)
	}
	events, err := node.GetEventsCh(c.GlobalString("topic-prefix") + ".#")
	if err != nil {
		return fmt.Errorf("can't get events channel from queue, %s", err)
	}
	cmdPipe, err := nagios.NewCmd(c.GlobalString("cmd-file"))
	if err != nil {
		log.Errorf("can't open nagios cmd file [%s], %s", c.GlobalString("cmd-file"), err)
		return fmt.Errorf("error opening cmd file")
	}
	selfcheck.UpdateStatus(nagios.StateOk, fmt.Sprintf("Running v%s", version))
	if !c.Bool("disable-selfcheck") {
		log.Notice("Generating selfcheck event every minute")
		go RunSelfcheck(node, c.GlobalString("topic-prefix")+".service.mq2nagcmd")
	}
	log.Notice("Connected to MQ and cmd file, entering main loop")
	stripFqdn := c.Bool("strip-fqdn")
	go func() {
		for ev := range events {
			if cmd, ok := ev.Headers["command"]; ok {
				send := false
				var cmdArgs string
				switch cmd {
				case nagios.CmdProcessHostCheckResult:
					host := nagios.NewHost()
					err := json.Unmarshal(ev.Body, &host)
					if err != nil {
						log.Warningf("Error when decoding host check: %s", err)
						continue
					}
					if stripFqdn && strings.Contains(host.Hostname, ".") {
						parts := strings.Split(host.Hostname, ".")
						host.Hostname = parts[0]
					}
					cmdArgs = host.MarshalCmd()
					send = true

				case nagios.CmdProcessServiceCheckResult:
					service := nagios.NewService()
					err := json.Unmarshal(ev.Body, &service)
					if err != nil {
						log.Warningf("Error when decoding host check: %s", err)
						continue
					}
					if stripFqdn && strings.Contains(service.Hostname, ".") {
						parts := strings.Split(service.Hostname, ".")
						service.Hostname = parts[0]
					}

					cmdArgs = service.MarshalCmd()
					send = true
				default:
					log.Warningf("Cmd not supported: %s", cmd)
				}
				if debug {
					log.Debugf("Got command [%s] with args [%s]", cmd, cmdArgs)
				}
				if send {
					err := cmdPipe.Send(cmd.(string), cmdArgs)
					if err != nil {
						log.Errorf("Error while writing to cmdfile, exiting: %s", err)
						end <- true
					}
				}

			} else {
				log.Warningf("Got unknown event with no 'command' header: %+v|%s", ev, ev.Body)
			}
		}
		log.Notice("MQ disconnected")
		end <- true

	}()
	_ = <-end
	log.Notice("Exiting main loop")
	return nil
}

func RunSelfcheck(node *zerosvc.Node, path string) {
	for {
		ev := utils.ServiceToEvent(node, selfcheck)
		ev.Headers["client-version"] = "mq2nagcmd-" + version
		if debug {
			log.Debugf("sending selfcheck to [%s]", path)
		}
		ev.Prepare()
		node.SendEvent(path, ev)
		time.Sleep(time.Minute)
	}
}
