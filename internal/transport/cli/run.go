package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/app"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"os"
	"strings"
)

const Version = "0.1.13-rc.1"
const help = `jev-router serve [--config file]
jev-router dashboard [--url URL] [--config file] [--no-open]
jev-router schema [capability] [--url URL] [--config file]
jev-router call <capability> --json <file|-> [--expected-version N --idempotency-key KEY]
Read calls also support --id ID, --cursor CURSOR, --limit N.
Settings token: configured env:/file: reference. Output: JSON; diagnostics: stderr.
`

func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	fail := func(e error) int {
		r := management.Failure(e)
		json.NewEncoder(out).Encode(r)
		return contracts.ErrorMapping(r.Error.Code).Exit
	}
	bad := func() int { return fail(routing.Fail("invalid_request", "invalid command arguments; use --help")) }
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, help)
		return 0
	}
	if args[0] == "--version" {
		fmt.Fprintln(out, Version)
		return 0
	}
	command := args[0]
	args = args[1:]
	id := ""
	if (command == "schema" || command == "call") && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	noOpen := f.Bool("no-open", false, "print dashboard URL only")
	config := f.String("config", "", "startup config")
	url := f.String("url", "", "instance URL")
	file := f.String("json", "", "input JSON file or -")
	resource := f.String("id", "", "resource ID")
	cursor := f.String("cursor", "", "cursor")
	limit := f.Int("limit", 0, "page size")
	version := f.Int64("expected-version", 0, "configuration version")
	key := f.String("idempotency-key", "", "write key")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return bad()
	}
	if command != "serve" && command != "schema" && command != "call" && command != "dashboard" {
		return bad()
	}
	if command != "dashboard" {
		invalid := false
		f.Visit(func(v *flag.Flag) {
			if v.Name == "no-open" {
				invalid = true
			}
		})
		if invalid {
			return bad()
		}
	}
	if command == "call" && id == "" {
		return bad()
	}
	cfg, e := app.LoadConfig(*config, os.LookupEnv)
	if e != nil {
		return fail(routing.Fail("config_missing", "startup configuration unavailable"))
	}
	if command == "serve" {
		if e := app.Run(ctx, cfg); e != nil {
			fmt.Fprintln(errOut, "server failed")
			return fail(routing.Fail("internal_error", "server failed"))
		}
		return 0
	}
	if command == "dashboard" {
		address, err := dashboardURL(*url, os.Getenv("JEV_ROUTER_URL"), cfg.Listen)
		if err != nil {
			return fail(routing.Fail("invalid_request", "invalid dashboard instance URL; use an http(s) root URL without credentials, query or fragment"))
		}
		return runDashboard(ctx, address, *noOpen, out, probeDashboard, openDashboard)
	}
	token, e := app.ResolveSecret(cfg.SettingsRef, os.LookupEnv, os.ReadFile)
	if e != nil {
		return fail(routing.Fail("config_missing", "settings credential unavailable"))
	}
	base := *url
	if base == "" {
		base = os.Getenv("JEV_ROUTER_URL")
	}
	if base == "" {
		base = "http://" + cfg.Listen
	}
	method, path := "GET", "/admin/v1/schema"
	var body any
	if command == "schema" {
		if id != "" {
			path += "/" + id
		}
	} else {
		method = "POST"
		path = "/admin/v1/call/" + id
		simple := map[string]any{}
		if *resource != "" {
			simple["id"] = *resource
		}
		if *cursor != "" {
			simple["cursor"] = *cursor
		}
		if *limit != 0 {
			simple["limit"] = *limit
		}
		var raw []byte
		if *file != "" {
			if len(simple) > 0 {
				return bad()
			}
			reader := in
			var handle *os.File
			if *file != "-" {
				handle, e = os.Open(*file)
				if e != nil {
					return fail(routing.Fail("invalid_request", "input file unavailable"))
				}
				defer handle.Close()
				reader = handle
			}
			if reader == nil {
				return bad()
			}
			raw, e = io.ReadAll(io.LimitReader(reader, (16<<20)+1))
			if e != nil || len(raw) > 16<<20 {
				return bad()
			}
		} else {
			raw, _ = json.Marshal(simple)
		}
		if _, e = contracts.Decode(raw); e != nil {
			return bad()
		}
		c := management.Call{Input: raw, IdempotencyKey: *key}
		if *version != 0 {
			c.ExpectedVersion = version
		}
		body = c
	}
	result, e := request(ctx, base, method, path, token, body)
	if e != nil {
		return fail(e)
	}
	json.NewEncoder(out).Encode(result)
	if !result.OK && result.Error != nil {
		return contracts.ErrorMapping(result.Error.Code).Exit
	}
	return 0
}
