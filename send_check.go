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
			Value: "check.results.",
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
			switch cmd {
			case nagios.CmdProcessHostCheckResult:
				host, err := nagios.NewHostFromArgs(args)
				if err != nil {
					log.Warningf("Error when parsing host check: %s")
					continue
				}
				ev := zerosvc.NewEvent()
				ev.Headers["host"] = host.Hostname
				ev.Body, err = json.Marshal(host)
				ev.Prepare()
				mq.SendEvent(c.GlobalString("topic-prefix")+".host."+host.Hostname, ev)

			case nagios.CmdProcessServiceCheckResult:
				service, err := nagios.NewServiceFromArgs(args)
				if err != nil {
					log.Warningf("Error when parsing service check: %s")
				}

				ev := zerosvc.NewEvent()
				ev.Headers["host"] = service.Hostname
				//ev.Body, err = json.Marshal(service)
				ev.Prepare()
				mq.SendEvent(c.GlobalString("topic-prefix")+".service."+service.Hostname, ev)
			}
		}
		return nil
	}
	app.Run(os.Args)

}
