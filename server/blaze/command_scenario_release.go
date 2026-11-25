//go:build !scenario

package blaze

import "github.com/darkspinnet/darkspin/server/sporenet"

func recordScenarioCommandReply(req *Request, user *sporenet.User, reply string) {}
