package cli

import (
	"os"
	"os/user"

	"github.com/ServloOfficial/servlo/internal/auditlog"
)

// Who a command-line action was taken by.
//
// The panel knows its actor from the session. A shell does not, so the honest
// answer is the Unix user running the command: on a box where one operator has
// the login and the rest reach the panel, that is exactly the distinction worth
// recording, and "someone with a shell" is more than an empty field says.
//
// No IP. The request came from a terminal on this machine, and recording a
// loopback address would suggest the entry knows something it does not.

// recordAudit appends an entry attributed to whoever is at the keyboard.
func recordAudit(e auditlog.Entry) {
	if e.Actor == "" {
		e.Actor = shellActor()
	}
	auditlog.Record(e)
}

// shellActor is the Unix user, by whatever means answers. A machine where none
// of them do is one where the entry is still worth writing without a name.
func shellActor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	for _, key := range []string{"SUDO_USER", "USER", "LOGNAME"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}
