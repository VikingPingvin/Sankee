package main

import (
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
)

func main() {
	fset := flag.NewFlagSet("sankey", flag.ExitOnError)
	printVersion := fset.Bool("version", false, "print version and exit")
	fset.String("config", "config.json", "path to JSON config file")
	fset.String("addr", "", "listen address (overrides config and env)")
	fset.Bool("debug_populate", false, "seed first workspace with sample data on startup")
	fset.String("db_driver", "", "database driver: sqlite or postgres (overrides config and env)")
	fset.String("db_dsn", "", "database DSN: file path for sqlite, connection string for postgres")
	fset.Parse(os.Args[1:])

	if *printVersion {
		fmt.Printf("sankee %s\n", version)
		os.Exit(0)
	}

	cfg := LoadConfig(fset)

	db, err := OpenDB(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Ensure at least one workspace exists before serving.
	workspaces, err := dbListWorkspaces(db)
	if err != nil {
		log.Fatalf("list workspaces: %v", err)
	}
	if len(workspaces) == 0 {
		if err := dbCreateWorkspace(db, "default", "Default"); err != nil {
			log.Fatalf("create default workspace: %v", err)
		}
	}

	// DebugPopulate seeds the first workspace when configured and empty.
	if cfg.DebugPopulate {
		store, err := LoadStore(db, "default")
		if err != nil {
			log.Fatalf("load store for seed: %v", err)
		}
		if len(store.All()) <= 1 {
			DebugPopulate(store)
		}
	}

	tmpl, err := template.ParseFS(staticFiles, "templates/index.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static sub: %v", err)
	}

	h := NewHandlers(db, tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.Index)
	mux.HandleFunc("GET /api/sankey-data", h.SankeyData)
	mux.HandleFunc("PUT /currency", h.SetCurrency)
	mux.HandleFunc("PUT /income", h.SetIncome)
	mux.HandleFunc("GET /nodes", h.Nodes)
	mux.HandleFunc("POST /nodes", h.CreateNode)
	mux.HandleFunc("PUT /nodes/{id}", h.UpdateNode)
	mux.HandleFunc("DELETE /nodes/{id}", h.DeleteNode)
	mux.HandleFunc("POST /workspaces", h.CreateWorkspace)
	mux.HandleFunc("DELETE /workspaces/{id}", h.DeleteWorkspace)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	log.Printf("Listening on http://%s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
