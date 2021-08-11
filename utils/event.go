package utils

import (
	"encoding/json"
	"github.com/efigence/go-nagios"
	"github.com/zerosvc/go-zerosvc"
)

func HostToEvent(node *zerosvc.Node, host nagios.Host) zerosvc.Event {
	ev := node.NewEvent()
	ev.Headers["command"] = nagios.CmdProcessHostCheckResult
	ev.Headers["host"] = host.Hostname
	ev.Body, _ = json.Marshal(host)
	return ev
}

func ServiceToEvent(node *zerosvc.Node, service nagios.Service) zerosvc.Event {
	ev := node.NewEvent()
	ev.Headers["command"] = nagios.CmdProcessServiceCheckResult
	ev.Headers["host"] = service.Hostname
	ev.Headers["service"] = service.Description
	ev.Body, _ = json.Marshal(service)
	return ev
}
