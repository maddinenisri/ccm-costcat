// ccm-costcat: fetch Harness CCM Cost Categories (business-mapping API),
// pretty-print JSON, and write raw / reshaped outputs to files.
//
// Env:   HARNESS_API_KEY, HARNESS_ACCOUNT_ID, HARNESS_BASE_URL (optional)
// Usage: ccm-costcat <command> [flags]
//
// Commands:
//
//	list          GET  /business-mapping (all pages)
//	get           GET  /business-mapping/{id}
//	create        POST /business-mapping            (-in file.json)
//	update        PUT  /business-mapping            (-in file.json)
//	delete        DELETE /business-mapping/{id}
//	filter-panel  GET  /business-mapping/filter-panel
//	linked-bm     POST /business-mapping/linkedBusinessMapping/{id}
//	linked-persp  POST /business-mapping/linkedPerspectives  (-id a -id b)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/maddinenisri/ccm-costcat/internal/harness"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

type common struct {
	apiKey    string
	accountID string
	baseURL   string
	outDir    string
	outFile   string
	format    string // raw | names | summary | flat | buckets | csv | all
	stdout    bool
	timeout   time.Duration
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)

	var c common
	fs.StringVar(&c.apiKey, "api-key", os.Getenv("HARNESS_API_KEY"), "Harness PAT/SAT (env HARNESS_API_KEY)")
	fs.StringVar(&c.accountID, "account", os.Getenv("HARNESS_ACCOUNT_ID"), "Harness account identifier (env HARNESS_ACCOUNT_ID)")
	fs.StringVar(&c.baseURL, "base-url", envOr("HARNESS_BASE_URL", harness.DefaultBaseURL), "API base URL")
	fs.StringVar(&c.outDir, "out-dir", "out", "output directory")
	fs.StringVar(&c.outFile, "out", "", "explicit output file (overrides -out-dir naming; raw format only)")
	fs.StringVar(&c.format, "format", "all", "raw | names (UUID and name only) | summary | flat | buckets | csv | all")
	fs.BoolVar(&c.stdout, "stdout", false, "also print pretty JSON to stdout")
	fs.DurationVar(&c.timeout, "timeout", 2*time.Minute, "overall timeout")

	// command-specific
	var (
		id        string
		ids       multiFlag
		inFile    string
		searchKey string
		sortType  string
		sortOrder string
		limit     int
		costCat   string
		search    string
	)
	fs.StringVar(&id, "id", "", "cost category uuid")
	fs.Var(&ids, "ids", "cost category uuid (repeatable)")
	fs.StringVar(&inFile, "in", "", "input JSON file (BusinessMapping) for create/update")
	fs.StringVar(&searchKey, "search-key", "", "list: searchKey")
	fs.StringVar(&sortType, "sort-type", "", "list: NAME | LAST_EDIT")
	fs.StringVar(&sortOrder, "sort-order", "", "list: ASCENDING | DESCENDING")
	fs.IntVar(&limit, "limit", 100, "list: page size")
	fs.StringVar(&costCat, "cost-category", "", "filter-panel: costCategory")
	fs.StringVar(&search, "search", "", "filter-panel: search")

	_ = fs.Parse(os.Args[2:])

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cl, err := harness.NewClient(c.apiKey, c.accountID, harness.WithBaseURL(c.baseURL))
	if err != nil {
		fatal(err)
	}

	switch cmd {
	case "list":
		runList(ctx, cl, c, harness.ListOptions{SearchKey: searchKey, SortType: sortType, SortOrder: sortOrder, Limit: limit})
	case "get":
		need(id, "-id")
		runGet(ctx, cl, c, id)
	case "create":
		need(inFile, "-in")
		runCreateUpdate(ctx, cl, c, inFile, false)
	case "update":
		need(inFile, "-in")
		runCreateUpdate(ctx, cl, c, inFile, true)
	case "delete":
		need(id, "-id")
		res, err := cl.Delete(ctx, id)
		must(err)
		emitRaw(c, "delete_"+id, res.Raw)
	case "filter-panel":
		res, err := cl.FilterPanel(ctx, costCat, search)
		must(err)
		emitRaw(c, "filter_panel", res.Raw)
	case "linked-bm":
		need(id, "-id")
		res, err := cl.LinkedBusinessMapping(ctx, id)
		must(err)
		emitRaw(c, "linked_business_mapping_"+id, res.Raw)
	case "linked-persp":
		if len(ids) == 0 {
			fatal(errors.New("at least one -ids is required"))
		}
		res, err := cl.LinkedPerspectives(ctx, ids)
		must(err)
		emitRaw(c, "linked_perspectives", res.Raw)
	default:
		usage()
		os.Exit(2)
	}
}

// ---- commands --------------------------------------------------------------

func runList(ctx context.Context, cl *harness.Client, c common, opts harness.ListOptions) {
	all, raws, err := cl.ListAll(ctx, opts)
	must(err)
	fmt.Fprintf(os.Stderr, "fetched %d cost categories (%d page(s))\n", len(all), len(raws))

	// Raw: single page -> as-is; multiple pages -> merged envelope.
	var raw []byte
	if len(raws) == 1 {
		raw = raws[0]
	} else {
		merged := harness.RestResponse[harness.BusinessMappingList]{
			Resource: harness.BusinessMappingList{BusinessMappings: all, TotalCount: len(all)},
		}
		raw, err = json.Marshal(merged)
		must(err)
	}
	emitAll(c, "cost_categories", raw, all)
}

func runGet(ctx context.Context, cl *harness.Client, c common, id string) {
	res, err := cl.Get(ctx, id)
	must(err)
	if res.Value.Resource == nil {
		fatal(fmt.Errorf("cost category %q not found (resource is null)", id))
	}
	emitAll(c, "cost_category_"+id, res.Raw, []harness.BusinessMapping{*res.Value.Resource})
}

func runCreateUpdate(ctx context.Context, cl *harness.Client, c common, inFile string, update bool) {
	b, err := os.ReadFile(inFile)
	must(err)
	var bm harness.BusinessMapping
	must(json.Unmarshal(b, &bm))

	var (
		res  harness.Result[json.RawMessage]
		name string
	)
	if update {
		res, err = cl.Update(ctx, bm)
		name = "update_" + bm.UUID
	} else {
		res, err = cl.Create(ctx, bm)
		name = "create_" + sanitize(bm.Name)
	}
	must(err)
	emitRaw(c, name, res.Raw)
}

// ---- output ----------------------------------------------------------------

func emitAll(c common, stem string, raw []byte, bms []harness.BusinessMapping) {
	want := func(f string) bool { return c.format == "all" || c.format == f }

	if want("raw") {
		emitRaw(c, stem, raw)
	}
	if want("names") {
		writeJSON(c, stem+"_names.json", harness.CategoryRefs(bms))
	}
	if want("summary") {
		writeJSON(c, stem+"_summary.json", harness.SummarizeAll(bms))
	}
	if want("flat") {
		writeJSON(c, stem+"_flat.json", harness.FlattenAll(bms))
	}
	if want("buckets") {
		writeJSON(c, stem+"_buckets.json", harness.ToBucketMap(bms))
	}
	if want("csv") {
		p := outPath(c, stem+"_flat.csv")
		f, err := os.Create(p)
		must(err)
		must(harness.WriteCSV(f, harness.FlattenAll(bms)))
		must(f.Close())
		fmt.Fprintln(os.Stderr, "wrote", p)
	}
}

func emitRaw(c common, stem string, raw []byte) {
	pretty, err := harness.PrettyRaw(raw)
	if err != nil { // non-JSON body (e.g. plain string) — keep as-is
		pretty = append(raw, '\n')
	}
	p := c.outFile
	if p == "" {
		p = outPath(c, stem+".json")
	}
	must(os.MkdirAll(filepath.Dir(p), 0o755))
	must(os.WriteFile(p, pretty, 0o644))
	fmt.Fprintln(os.Stderr, "wrote", p)
	if c.stdout {
		os.Stdout.Write(pretty)
	}
}

func writeJSON(c common, name string, v any) {
	b, err := harness.PrettyJSON(v)
	must(err)
	p := outPath(c, name)
	must(os.WriteFile(p, b, 0o644))
	fmt.Fprintln(os.Stderr, "wrote", p)
	if c.stdout {
		os.Stdout.Write(b)
	}
}

func outPath(c common, name string) string {
	must(os.MkdirAll(c.outDir, 0o755))
	return filepath.Join(c.outDir, name)
}

// ---- helpers ---------------------------------------------------------------

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func sanitize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func need(v, flagName string) {
	if v == "" {
		fatal(fmt.Errorf("%s is required", flagName))
	}
}

func must(err error) {
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	var he *harness.HTTPError
	if errors.As(err, &he) && len(he.Body) > 0 {
		if pretty, perr := harness.PrettyRaw(he.Body); perr == nil {
			os.Stderr.Write(pretty)
		}
	}
	os.Exit(1)
}

func usage() {
	fmt.Fprint(os.Stderr, `ccm-costcat - Harness CCM Cost Categories (business-mapping API)

Usage:
  ccm-costcat <command> [flags]

Commands:
  list           list all cost categories (paged)   [-search-key -sort-type -sort-order -limit]
  get            get one by id                       -id <uuid>
  create         create from JSON file               -in <file.json>
  update         update from JSON file (uuid set)    -in <file.json>
  delete         delete by id                        -id <uuid>
  filter-panel   list category names for filters     [-cost-category -search]
  linked-bm      categories referencing this one     -id <uuid>
  linked-persp   perspectives using these categories -ids <uuid> [-ids <uuid>...]

Common flags:
  -api-key    (env HARNESS_API_KEY)      -account   (env HARNESS_ACCOUNT_ID)
  -base-url   (env HARNESS_BASE_URL, default `+harness.DefaultBaseURL+`)
  -out-dir    output dir (default "out") -out       explicit raw output file
  -format     raw|names|summary|flat|buckets|csv|all (default all; list/get only)
              names: JSON array containing only uuid and name
  -stdout     echo output to stdout      -timeout   (default 2m)
`)
}
