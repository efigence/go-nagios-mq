package main

import (
	"fmt"
	"os"

	"bufio"

	"encoding/json"
	"strings"
	"time"

	nagios "github.com/efigence/go-nagios"
	"github.com/efigence/go-nagios-mq/utils"
	"github.com/op/go-logging"
	"github.com/urfave/cli"
	"github.com/zerosvc/go-zerosvc"
)

var version string
var debug bool
var log = logging.MustGetLogger("main")
var stdout_log_format = logging.MustStringFormatter("%{color:bold}%{time:2006-01-02T15:04:05.0000Z-07:00}%{color:reset}%{color} [%{level:.1s}] %{color:reset}%{shortpkg}[%{longfunc}] %{message}")

func main() {
	stderrBackend := logging.NewLogBackend(os.Stderr, "", 0)
	stderrFormatter := logging.NewBackendFormatter(stderrBackend, stdout_log_format)
	logging.SetBackend(stderrFormatter)
	logging.SetFormatter(stdout_log_format)

	app := cli.NewApp()
	app.Name = "send_check"
	app.Description = "Send check results to monitoring system"
	app.Version = version
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:     "amqp-url",
			Value:    "amqp://guest:guest@localhost:5672/",
			Usage:    "AMQP url-flag",
			EnvVar:   "AMQP_URL",
			FilePath: "/etc/nagios/send_check_url",
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
		cli.StringFlag{
			Name:  "exchange",
			Value: "monitoring",
			Usage: "exchange to receive events on",
		},
		cli.BoolFlag{
			Name:  "emulate-send-nsca",
			Usage: "Emulate how send_nsca works (tab-delimited, guess if it is host or service check based on number of elements",
		},
		cli.BoolFlag{
			Name:  "debug",
			Usage: "Turn on debug",
		},
	}
	app.Action = func(c *cli.Context) error {
		debug = c.Bool("debug")
		if debug {
			log.Infof("Starting app version: %s", version)
		}
		hostname, _ := os.Hostname()
		mq, err := zerosvc.New("send-check@"+hostname,
			zerosvc.TransportAMQP(
				strings.TrimSuffix(c.GlobalString("amqp-url"), "\n"),
				zerosvc.TransportAMQPConfig{
					EventExchange: c.GlobalString("exchange"),
				},
			),
		)
		if err != nil {
			log.Errorf("can't connect to queue: %s", err)
			os.Exit(1)
		}
		scanner := bufio.NewScanner(os.Stdin)
		var LineHandler func(mq *zerosvc.Node, c *cli.Context, line string) (err error)
		if c.Bool("emulate-send-nsca") {
			LineHandler = HandleSendNcsa
		} else {
			LineHandler = HandleNagiosCmdLine
		}
		for scanner.Scan() {
			line := scanner.Text()
			err := LineHandler(mq, c, line)
			if err != nil {
				log.Errorf("error when parsing [%s]: %s", line, err)
			}
		}
		return nil
	}
	app.Run(os.Args)

}

func HandleNagiosCmdLine(mq *zerosvc.Node, c *cli.Context, line string) (err error) {
	cmd, args, err := nagios.DecodeNagiosCmd(line)
	if err != nil {
		return fmt.Errorf("bad cmd: %s", err)
	}
	var ev zerosvc.Event
	var path string
	switch cmd {
	case nagios.CmdProcessHostCheckResult:
		host, err := nagios.NewHostFromArgs(args)
		host.LastCheck = time.Now()
		if err != nil {
			return fmt.Errorf("Error when parsing host check: %s")
		}
		ev := utils.HostToEvent(mq, host)
		ev.Headers["host"] = host.Hostname
		ev.Body, _ = json.Marshal(host)
		path = c.GlobalString("topic-prefix") + ".host." + host.Hostname

	case nagios.CmdProcessServiceCheckResult:
		service, err := nagios.NewServiceFromArgs(args)
		service.LastCheck = time.Now()
		if err != nil {
			return fmt.Errorf("Error when parsing service check: %s")
		}
		ev = utils.ServiceToEvent(mq, service)
		path = c.GlobalString("topic-prefix") + ".service." + service.Hostname
	default:
		if cmd == strings.ToUpper(cmd) && len(args) > 0 {
			path = c.GlobalString("topic-prefix") + ".command"
			ev.Body, _ = json.Marshal(args)
		} else {
			return fmt.Errorf("unsupported cmd [%s] with args [%+v]", cmd, args)
		}

	}
	ev.Headers["client-version"] = "send_check-" + version
	ev.Headers["command"] = cmd
	return mq.SendEvent(path, ev)
}

func HandleSendNcsa(mq *zerosvc.Node, c *cli.Context, line string) (err error) {
	args := strings.Split(line, "\t")
	var cmd string
	var path string
	var ev zerosvc.Event
	if len(args) == 4 {
		cmd = nagios.CmdProcessServiceCheckResult
		service, err := nagios.NewServiceFromArgs(args)
		service.LastCheck = time.Now()
		if err != nil {
			return fmt.Errorf("Error when parsing service check: %s")
		}
		ev = utils.ServiceToEvent(mq, service)
		path = c.GlobalString("topic-prefix") + ".service." + service.Hostname

	} else if len(args) == 3 {
		cmd = nagios.CmdProcessHostCheckResult
		host, err := nagios.NewHostFromArgs(args)
		host.LastCheck = time.Now()
		if err != nil {
			return fmt.Errorf("Error when parsing host check: %s")
		}
		ev := utils.HostToEvent(mq, host)
		ev.Headers["host"] = host.Hostname
		ev.Body, _ = json.Marshal(host)
		path = c.GlobalString("topic-prefix") + ".host." + host.Hostname
	} else {
		return fmt.Errorf("Can't parse [%s]")
	}
	ev.Headers["client-version"] = "send_check-" + version
	ev.Headers["command"] = cmd
	if debug {
		log.Debugf("Will send [%+v] to [%s]", ev, path)
	}
	return mq.SendEvent(path, ev)
	return nil
}
