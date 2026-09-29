package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen, Database, SettingsRef, InferenceRef                    string
	DecisionTimeout, FirstEventTimeout, IdleTimeout, RecordTimeout time.Duration
	MaxBodyBytes                                                   int64
	StreamBuffer, RetentionDays                                    int
}

func LoadConfig(path string, lookup func(string) (string, bool)) (Config, error) {
	c := Config{Listen: "127.0.0.1:8080", Database: ".data/router.sqlite", SettingsRef: "env:JEV_ROUTER_SETTINGS_TOKEN", InferenceRef: "env:JEV_ROUTER_INFERENCE_TOKEN", DecisionTimeout: 10 * time.Second, FirstEventTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, RecordTimeout: 100 * time.Millisecond, MaxBodyBytes: 16 << 20, StreamBuffer: 8, RetentionDays: 7}
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
	stringsMap := map[string]*string{"listen": &c.Listen, "database": &c.Database, "settings_token_ref": &c.SettingsRef, "inference_token_ref": &c.InferenceRef}
	durations := map[string]*time.Duration{"decision_timeout": &c.DecisionTimeout, "first_event_timeout": &c.FirstEventTimeout, "idle_timeout": &c.IdleTimeout, "record_timeout": &c.RecordTimeout}
	numbers := map[string]bool{"max_body_bytes": true, "stream_buffer": true, "retention_days": true}
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
		if *p == "" {
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
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 1 || n > 256<<20 {
			return c, errors.New("invalid positive numeric limit")
		}
		switch k {
		case "max_body_bytes":
			c.MaxBodyBytes = n
		case "stream_buffer":
			if n > 1024 {
				return c, errors.New("stream buffer too large")
			}
			c.StreamBuffer = int(n)
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
	return c, nil
}
