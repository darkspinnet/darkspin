//go:build scenario

package blaze

import (
	"encoding/json"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// This is queue evidence, not proof of client receipt or rendered chat text.
// Only scenario binaries retain exact command/reply text in their owned log.
func recordScenarioCommandReply(req *Request, user *sporenet.User, reply string) {
	if req == nil || req.Session == nil || req.Session.server == nil || req.Session.server.logger == nil || user == nil {
		return
	}
	body, isBodyFound := messagingBody(req.Fields)
	if !isBodyFound {
		return
	}
	command, isCommand := messagingCommandName(body)
	if !isCommand {
		return
	}
	fields := strings.Fields(body)
	fields[0] = command
	normalizedBody := strings.Join(fields, " ")
	if len(normalizedBody) > 4096 || len(reply) > 16384 {
		req.Session.server.logger.Printf("Scenario command reply exceeds observation bound")
		return
	}
	event := struct {
		UserID    int64  `json:"user_id"`
		SessionID uint32 `json:"session_id"`
		Command   string `json:"command"`
		Body      string `json:"body"`
		Reply     string `json:"reply"`
	}{user.Account.ID, req.Session.ID, command, normalizedBody, reply}
	payload, err := json.Marshal(event)
	if err != nil {
		req.Session.server.logger.Printf("Scenario command reply marshal failed: %v", err)
		return
	}
	req.Session.server.logger.Printf("ScenarioCommandReply %s", payload)
}
