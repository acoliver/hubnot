package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

const serveRootRefusal = "hn serve refuses to run as root; create an unprivileged user for the server"

// serveRoute is one hn serve subcommand. run receives a layout that cmdServe
// has already validated in mode, so no subcommand can skip validation.
type serveRoute struct {
	mode serveValidationMode
	run  func(layout serveLayout, args []string) error
}

// lookupServeRoute is a variable so in-process tests can register a probe
// route and observe the dispatcher's checks.
var lookupServeRoute = serveRouteFor

// serveRouteFor maps a subcommand to its route. Each later work package adds
// one case here, for example:
//
//	case "repo": return serveRoute{mode: serveAdministrationValidation, run: cmdServeRepo}, true
//	case "git":  return serveRoute{mode: serveSessionValidation, run: cmdServeGit}, true
//
// repo and key (WP05) use administration mode; git (WP06) and hook (WP07)
// use session mode.
func serveRouteFor(subcommand string) (serveRoute, bool) {
	switch subcommand {
	}
	return serveRoute{}, false
}

func cmdServe(args []string) error {
	if err := refuseServeAsRoot(); err != nil {
		return err
	}
	if len(args) == 0 {
		return usageError("usage: hn serve repo|key|git|hook ...")
	}
	route, ok := lookupServeRoute(args[0])
	if !ok {
		return fmt.Errorf("unknown hn serve subcommand %q; run 'hn help'", args[0])
	}
	layout, err := resolveServeLayout()
	if err != nil {
		return err
	}
	if err := prepareServeLayout(layout, route.mode); err != nil {
		return err
	}
	return route.run(layout, args[1:])
}

// prepareServeLayout creates the fixed directories for administration
// commands, then validates the whole layout. Session commands only validate.
func prepareServeLayout(layout serveLayout, mode serveValidationMode) error {
	if mode == serveAdministrationValidation {
		if err := ensureServeLayout(layout); err != nil {
			return err
		}
	}
	return validateServeLayout(layout, mode)
}

// refuseServeAsRoot runs before any path is resolved or state is read.
func refuseServeAsRoot() error {
	euid, err := serveEffectiveUID()
	if err != nil {
		return err
	}
	if euid == 0 {
		return errors.New(serveRootRefusal)
	}
	return nil
}

// serveEffectiveUID honours HN_TEST_SERVE_EUID only when HN_INTERNAL_TESTING=1
// so tests on an ordinary account can exercise the root refusal.
func serveEffectiveUID() (int, error) {
	if os.Getenv("HN_INTERNAL_TESTING") == "1" {
		if value, ok := os.LookupEnv("HN_TEST_SERVE_EUID"); ok {
			return parseServeTestUID(value)
		}
	}
	return os.Geteuid(), nil
}

func parseServeTestUID(value string) (int, error) {
	malformed := fmt.Errorf("HN_TEST_SERVE_EUID must be a decimal user ID; got %q", value)
	if value == "" {
		return 0, malformed
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, malformed
		}
	}
	uid, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, malformed
	}
	return int(uid), nil
}
