package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/decision"
	"github.com/zerone-agents/jev-model-router/internal/playground"
	httptransport "github.com/zerone-agents/jev-model-router/internal/transport/http"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Playground                                                     playground.Limits
	DecisionMaxBytes                                               int
	DashboardOrigin                                                string
	EncryptionKeyRef                                               string
	Listen, Database, SettingsRef, InferenceRef                    string
	DecisionTimeout, FirstEventTimeout, IdleTimeout, RecordTimeout time.Duration
	MaxBodyBytes                                                   int64
	StreamBuffer, RetentionDays, RecordMaxCount                    int
}

func LoadConfig(path string, lookup func(string) (string, bool)) (Config, error) {
	c := Config{Playground: playground.DefaultLimits(), DecisionMaxBytes: decision.DefaultMaxBytes, Listen: "127.0.0.1:8080", Database: ".data/router.sqlite", SettingsRef: "env:JEV_ROUTER_SETTINGS_TOKEN", InferenceRef: "env:JEV_ROUTER_INFERENCE_TOKEN", DecisionTimeout: 10 * time.Second, FirstEventTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, RecordTimeout: 100 * time.Millisecond, MaxBodyBytes: 16 << 20, StreamBuffer: 8, RetentionDays: 7, RecordMaxCount: 100000}
	values := map[string]json.RawMessage{}
	if path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return c, errors.New("cannot read configuration file")
		}
		d := json.NewDecoder(bytes.NewReader(b))
		if e = d.Decode(&values); e != nil {
			return c, errors.New("invalid JSON configuration")
		}
	}
	stringsMap := map[string]*string{"dashboard_origin": &c.DashboardOrigin, "encryption_key_ref": &c.EncryptionKeyRef, "listen": &c.Listen, "database": &c.Database, "settings_token_ref": &c.SettingsRef, "inference_token_ref": &c.InferenceRef}
	durations := map[string]*time.Duration{"decision_timeout": &c.DecisionTimeout, "first_event_timeout": &c.FirstEventTimeout, "idle_timeout": &c.IdleTimeout, "record_timeout": &c.RecordTimeout}
	numbers := map[string]bool{"decision_max_bytes": true, "max_body_bytes": true, "stream_buffer": true, "retention_days": true, "record_max_count": true}
	for k := range c.Playground.Numbers() {
		numbers["playground_"+k] = true
	}
	numbers["playground_enabled"] = true
	durations["playground_timeout"] = &c.Playground.Timeout
	for k := range values {
		if stringsMap[k] == nil && durations[k] == nil && !numbers[k] {
			return c, errors.New("unknown configuration field")
		}
	}
	get := func(k string) (string, bool, error) {
		if v, ok := lookup("JEV_ROUTER_" + strings.ToUpper(k)); ok {
			return v, true, nil
		}
		if b, ok := values[k]; ok {
			var s string
			if json.Unmarshal(b, &s) == nil {
				return s, true, nil
			}
			if numbers[k] {
				return string(b), true, nil
			}
			return "", true, errors.New("configuration value must be a string")
		}
		return "", false, nil
	}
	for k, p := range stringsMap {
		v, ok, e := get(k)
		if e != nil {
			return c, e
		}
		if ok {
			*p = v
		}
		if *p == "" && k != "encryption_key_ref" && k != "dashboard_origin" {
			return c, errors.New("empty startup setting")
		}
	}
	for k, p := range durations {
		v, ok, e := get(k)
		if e != nil {
			return c, e
		}
		if ok {
			*p, e = time.ParseDuration(v)
			if e != nil || *p <= 0 {
				return c, errors.New("invalid positive duration")
			}
		}
	}
	for k := range numbers {
		v, ok, e := get(k)
		if e != nil {
			return c, e
		}
		if !ok {
			continue
		}
		if k == "playground_enabled" {
			if v != "true" && v != "false" {
				return c, errors.New("invalid playground_enabled boolean")
			}
			c.Playground.Enabled = v == "true"
			continue
		}
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 1 || n > 256<<20 {
			return c, errors.New("invalid positive numeric limit")
		}
		if p := c.Playground.Numbers()[strings.TrimPrefix(k, "playground_")]; strings.HasPrefix(k, "playground_") && p != nil {
			*p = int(n)
			continue
		}
		switch k {
		case "decision_max_bytes":
			c.DecisionMaxBytes = int(n)
		case "max_body_bytes":
			c.MaxBodyBytes = n
		case "stream_buffer":
			if n > 1024 {
				return c, errors.New("stream buffer too large")
			}
			c.StreamBuffer = int(n)
		case "record_max_count":
			if n > 10000000 {
				return c, errors.New("record count too large")
			}
			c.RecordMaxCount = int(n)
		case "retention_days":
			if n > 3650 {
				return c, errors.New("retention too large")
			}
			c.RetentionDays = int(n)
		}
	}
	if _, _, e := net.SplitHostPort(c.Listen); e != nil {
		return c, errors.New("invalid listen address")
	}
	if e := c.Playground.Validate(); e != nil {
		return c, e
	}
	var err error
	c.DashboardOrigin, err = httptransport.NormalizeDashboardOrigin(c.DashboardOrigin)
	return c, err
}
