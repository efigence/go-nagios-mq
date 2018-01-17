package main

import (
	"fmt"
	"os"

	"bufio"

	"encoding/json"
	nagios "github.com/efigence/go-nagios"
	"github.com/op/go-logging"
	"github.com/urfave/cli"
	"github.com/zerosvc/go-zerosvc"
	"strings"
	"time"
)

var version string
var log = logging.MustGetLogger("main")
var stdout_log_format = logging.MustStringFormatter("%{color:bold}%{time:2006-01-02T15:04:05.0000Z-07:00}%{color:reset}%{color} [%{level:.1s}] %{color:reset}%{shortpkg}[%{longfunc}] %{message}")

func main() {
	stderrBackend := logging.NewLogBackend(os.Stderr, "", 0)
	stderrFormatter := logging.NewBackendFormatter(stderrBackend, stdout_log_format)
	logging.SetBackend(stderrFormatter)
	logging.SetFormatter(stdout_log_format)

	log.Infof("Starting app version: %s", version)
	app := cli.NewApp()
	app.Name = "send_check"
	app.Description = "Send check results to monitoring system"
	app.Version = version
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "amqp-url",
			Value:  "amqp://guest:guest@localhost:5672/",
			Usage:  "AMQP url-flag",
			EnvVar: "AMQP_URL",
		},
		cli.StringFlag{
			Name:  "host, H",
			Usage: "IGNORED, this exists only to maintain compability with send_nsca",
		},
		cli.StringFlag{
			Name:  "topic-prefix,topic",
			Value: "check.results",
			Usage: "topic prefix to send msg to",
		},
	}
	app.Action = func(c *cli.Context) error {
		hostname, _ := os.Hostname()
		mq := zerosvc.NewNode("send_check@" + hostname)
		tr := zerosvc.NewTransport(zerosvc.TransportAMQP, c.GlobalString("amqp-url"))
		err := tr.Connect()
		if err != nil {
			fmt.Errorf("Error when connection to M")
		}
		mq.SetTransport(tr)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			cmd, args, err := nagios.DecodeNagiosCmd(line)
			if err != nil {
				log.Warningf("bad cmd: %s", err)
				continue
			}
			ev := zerosvc.NewEvent()
			var path string
			switch cmd {
			case nagios.CmdProcessHostCheckResult:
				host, err := nagios.NewHostFromArgs(args)
				host.LastCheck = time.Now()
				if err != nil {
					log.Warningf("Error when parsing host check: %s")
					continue
				}
				ev.Headers["host"] = host.Hostname
				ev.Body, _ = json.Marshal(host)
				path = c.GlobalString("topic-prefix") + ".host." + host.Hostname

			case nagios.CmdProcessServiceCheckResult:
				service, err := nagios.NewServiceFromArgs(args)
				service.LastCheck = time.Now()
				if err != nil {
					log.Warningf("Error when parsing service check: %s")
				}

				ev.Headers["host"] = service.Hostname
				ev.Headers["service"] = service.Description
				ev.Body, _ = json.Marshal(service)
				path = c.GlobalString("topic-prefix") + ".service." + service.Hostname
			default:
				if cmd == strings.ToUpper(cmd) && len(args) > 0 {
					path = c.GlobalString("topic-prefix") + ".command"
					ev.Body, _ = json.Marshal(args)
				} else {
					err = fmt.Errorf("unsupported cmd [%s] with args [%+v]", cmd, args)
				}

			}
			ev.Headers["command"] = cmd
			if err != nil {
				log.Warningf("Error when sending command: %s", err)
				continue
			}
			//event, err = json.Marshal(ev)
			if err != nil {
				log.Warningf("Marshall error: %s", err)
				continue
			}
			mq.SendEvent(path, ev)
		}
		return nil
	}
	app.Run(os.Args)

}
