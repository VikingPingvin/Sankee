package main

import (
	"flag"
	"log"
	"os"
	"strings"

	kjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"
)

// Config holds all runtime configuration.
type Config struct {
	Addr          string `koanf:"addr"`
	DebugPopulate bool   `koanf:"debug_populate"`
	DBDriver      string `koanf:"db_driver"`
	DBDSN         string `koanf:"db_dsn"`
}

// LoadConfig builds a Config by layering providers in precedence order:
//
//	hardcoded defaults < config file < env vars (SANKEE_*) < CLI flags
//
// fs must have been populated with flag definitions and fs.Parse() called
// before LoadConfig is invoked.
func LoadConfig(fs *flag.FlagSet) Config {
	k := koanf.New(".")

	// 1. Hardcoded defaults.
	k.Load(confmap.Provider(map[string]interface{}{
		"addr":           "localhost:8080",
		"debug_populate": false,
		"db_driver":      "sqlite",
		"db_dsn":         "sankee.db",
	}, "."), nil)

	// 2. Config file — silent on not-found, logged on any other error.
	cfgPath := "config.json"
	if f := fs.Lookup("config"); f != nil {
		cfgPath = f.Value.String()
	}
	if err := k.Load(file.Provider(cfgPath), kjson.Parser()); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: %s: %v — using defaults", cfgPath, err)
		}
	}

	// 3. Environment variables.
	//    SANKEE_ADDR           → "addr"
	//    SANKEE_DEBUG_POPULATE → "debug_populate"
	k.Load(env.Provider("SANKEE_", ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, "SANKEE_"))
	}), nil)

	// 4. CLI flags — only flags explicitly set by the user win.
	//    Passing k as KeyMap prevents flag defaults from overwriting
	//    values already loaded from the file or env layer.
	k.Load(basicflag.Provider(fs, ".", &basicflag.Opt{KeyMap: k}), nil)

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		log.Printf("config: unmarshal error: %v — using defaults", err)
	}
	return cfg
}
