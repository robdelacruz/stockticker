package main

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type PrintFunc func(format string, a ...interface{}) (n int, err error)

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	sw, parms := parseArgs(args)

	// [-i new_file]  Create and initialize db file
	if sw["i"] != "" {
		dbfile := sw["i"]
		if fileExists(dbfile) {
			return fmt.Errorf("File '%s' already exists. Can't initialize it.\n", dbfile)
		}
		createTables(dbfile)
		return nil
	}

	// Need to specify a db file as first parameter.
	if len(parms) == 0 {
		s := `Usage:

Start webservice using database file:
	t2 <sites.db>

Initialize new database file:
	t2 -i <sites.db>
`
		fmt.Printf(s)
		return nil
	}

	// Exit if db file doesn't exist.
	dbfile := parms[0]
	if !fileExists(dbfile) {
		return fmt.Errorf(`Sites database file '%s' doesn't exist. Create one using:
	wb -i <notes.db>
`, dbfile)
	}
	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		return fmt.Errorf("Error opening '%s' (%s)\n", dbfile, err)
	}

	mc := MemCache{}
	dbc := DbCache(*db)
	err = dbc.CreateTables()
	if err != nil {
		return fmt.Errorf("Error creating dbcache table (%s)\n", err)
	}
	gob.Register(CacheEntry{})
	gob.Register(Overview{})
	gob.Register(Price{})

	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./static/coffee.ico") })
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	http.Handle("/", http.StripPrefix("/", http.FileServer(http.Dir("./"))))
	http.HandleFunc("/api/lookup/", lookupHandler(db, mc, dbc))

	port := "8000"
	fmt.Printf("Listening on %s...\n", port)
	err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	return err
}

func createTables(newfile string) {
	if fileExists(newfile) {
		s := fmt.Sprintf("File '%s' already exists. Can't initialize it.\n", newfile)
		fmt.Printf(s)
		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", newfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", newfile, err)
		os.Exit(1)
	}

	ss := []string{
		"CREATE TABLE user (user_id INTEGER PRIMARY KEY NOT NULL, username TEXT UNIQUE, password TEXT, active INTEGER NOT NULL, email TEXT);",
		"INSERT INTO user (user_id, username, password, active, email) VALUES (1, 'admin', '', 1, '');",
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("DB error (%s)\n", err)
		os.Exit(1)
	}
	for _, s := range ss {
		_, err := txexec(tx, s)
		if err != nil {
			tx.Rollback()
			log.Printf("DB error (%s)\n", err)
			os.Exit(1)
		}
	}
	err = tx.Commit()
	if err != nil {
		log.Printf("DB error (%s)\n", err)
		os.Exit(1)
	}
}

//*** DB functions ***
func sqlstmt(db *sql.DB, s string) *sql.Stmt {
	stmt, err := db.Prepare(s)
	if err != nil {
		log.Fatalf("db.Prepare() sql: '%s'\nerror: '%s'", s, err)
	}
	return stmt
}
func sqlexec(db *sql.DB, s string, pp ...interface{}) (sql.Result, error) {
	stmt := sqlstmt(db, s)
	defer stmt.Close()
	return stmt.Exec(pp...)
}
func txstmt(tx *sql.Tx, s string) *sql.Stmt {
	stmt, err := tx.Prepare(s)
	if err != nil {
		log.Fatalf("tx.Prepare() sql: '%s'\nerror: '%s'", s, err)
	}
	return stmt
}
func txexec(tx *sql.Tx, s string, pp ...interface{}) (sql.Result, error) {
	stmt := txstmt(tx, s)
	defer stmt.Close()
	return stmt.Exec(pp...)
}

//*** Helper functions ***

// Helper function to make fmt.Fprintf(w, ...) calls shorter.
// Ex.
// Replace:
//   fmt.Fprintf(w, "<p>Some text %s.</p>", str)
//   fmt.Fprintf(w, "<p>Some other text %s.</p>", str)
// with the shorter version:
//   P := makeFprintf(w)
//   P("<p>Some text %s.</p>", str)
//   P("<p>Some other text %s.</p>", str)
func makeFprintf(w io.Writer) func(format string, a ...interface{}) (n int, err error) {
	return func(format string, a ...interface{}) (n int, err error) {
		return fmt.Fprintf(w, format, a...)
	}
}
func listContains(ss []string, v string) bool {
	for _, s := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func fileExists(file string) bool {
	_, err := os.Stat(file)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}
func makePrintFunc(w io.Writer) func(format string, a ...interface{}) (n int, err error) {
	// Return closure enclosing io.Writer.
	return func(format string, a ...interface{}) (n int, err error) {
		return fmt.Fprintf(w, format, a...)
	}
}
func atoi(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
func idtoi(sid string) int64 {
	return int64(atoi(sid))
}
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
func atof(s string) float64 {
	if s == "" {
		return 0.0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return f
}

func parseArgs(args []string) (map[string]string, []string) {
	switches := map[string]string{}
	parms := []string{}

	standaloneSwitches := []string{}
	definitionSwitches := []string{"i"}
	fNoMoreSwitches := false
	curKey := ""

	for _, arg := range args {
		if fNoMoreSwitches {
			// any arg after "--" is a standalone parameter
			parms = append(parms, arg)
		} else if arg == "--" {
			// "--" means no more switches to come
			fNoMoreSwitches = true
		} else if strings.HasPrefix(arg, "--") {
			switches[arg[2:]] = "y"
			curKey = ""
		} else if strings.HasPrefix(arg, "-") {
			if listContains(definitionSwitches, arg[1:]) {
				// -a "val"
				curKey = arg[1:]
				continue
			}
			for _, ch := range arg[1:] {
				// -a, -b, -ab
				sch := string(ch)
				if listContains(standaloneSwitches, sch) {
					switches[sch] = "y"
				}
			}
		} else if curKey != "" {
			switches[curKey] = arg
			curKey = ""
		} else {
			// standalone parameter
			parms = append(parms, arg)
		}
	}

	return switches, parms
}

func handleErr(w http.ResponseWriter, err error, sfunc string) {
	log.Printf("%s: server error (%s)\n", sfunc, err)
	http.Error(w, "Server error.", 500)
}
func handleDbErr(w http.ResponseWriter, err error, sfunc string) bool {
	if err == sql.ErrNoRows {
		http.Error(w, "Not found.", 404)
		return true
	}
	if err != nil {
		log.Printf("%s: database error (%s)\n", sfunc, err)
		http.Error(w, "Server database error.", 500)
		return true
	}
	return false
}
func handleTxErr(tx *sql.Tx, err error) bool {
	if err != nil {
		tx.Rollback()
		return true
	}
	return false
}

//*** Cache functions ***
type Cache interface {
	Lookup(section, id string) interface{}
	Set(section, id string, item interface{}, minsMaxAge int)
	Remove(section, id string)
	Reset()
}
type CacheEntry struct {
	Item    interface{}
	Expires time.Time
}

func cachekey(section, id string) string {
	return fmt.Sprintf("%s_%s", section, id)
}

type MemCache map[string]*CacheEntry

func (mc MemCache) Lookup(section, id string) interface{} {
	k := cachekey(section, id)
	ce := mc[k]
	if ce == nil {
		return nil
	}
	if time.Now().After(ce.Expires) {
		return nil
	}
	return ce.Item
}
func (mc MemCache) Set(section, id string, item interface{}, minsMaxAge int) {
	k := cachekey(section, id)
	expires := time.Now().Add(time.Minute * time.Duration(minsMaxAge))
	mc[k] = &CacheEntry{Item: item, Expires: expires}
}
func (mc MemCache) Remove(section, id string) {
	delete(mc, id)
}
func (mc MemCache) Reset() {
	for k := range mc {
		delete(mc, k)
	}
}

type DbCache sql.DB

func (dbc DbCache) CreateTables() error {
	db := sql.DB(dbc)
	s := "CREATE TABLE IF NOT EXISTS dbcache (key TEXT PRIMARY KEY NOT NULL, content BLOB);"
	_, err := sqlexec(&db, s)
	return err
}
func (dbc DbCache) Lookup(section, id string) interface{} {
	db := sql.DB(dbc)
	k := cachekey(section, id)
	s := "SELECT content FROM dbcache WHERE key = ?"
	row := db.QueryRow(s, k)

	var bs []byte
	err := row.Scan(&bs)
	if err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return nil
	}
	ce := gobdecode(bs)
	if ce == nil {
		return nil
	}
	if time.Now().After(ce.Expires) {
		return nil
	}
	return ce.Item
}
func (dbc DbCache) Set(section, id string, item interface{}, minsMaxAge int) {
	db := sql.DB(dbc)
	k := cachekey(section, id)
	s := "INSERT OR REPLACE INTO dbcache (key, content) VALUES (?, ?)"

	expires := time.Now().Add(time.Minute * time.Duration(minsMaxAge))
	ce := CacheEntry{Item: item, Expires: expires}
	sqlexec(&db, s, k, gobencode(ce))
}
func (dbc DbCache) Remove(section, id string) {
	db := sql.DB(dbc)
	k := cachekey(section, id)
	s := "DELETE FROM dbcache WHERE key = ?"
	sqlexec(&db, s, k)
}
func (dbc DbCache) Reset() {
	db := sql.DB(dbc)
	s := "DELETE FROM dbcache"
	sqlexec(&db, s)
}

func gobencode(v CacheEntry) []byte {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	err := e.Encode(v)
	if err != nil {
		return nil
	}
	return b.Bytes()
}
func gobdecode(bs []byte) *CacheEntry {
	var v CacheEntry
	b := bytes.NewBuffer(bs)
	d := gob.NewDecoder(b)
	err := d.Decode(&v)
	if err != nil {
		return nil
	}
	return &v
}

type AlphaVantageOverview struct {
	Symbol      string `json:"Symbol"`
	AssetType   string `json:"AssetType"`
	Name        string `json:"Name"`
	Description string `json:"Description"`
	Exchange    string `json:"Exchange"`
}

func fetchOverview(sym string, cache Cache) (*Overview, error) {
	cachedOverview := cache.Lookup("overview", sym)
	if cachedOverview != nil {
		log.Printf("(Returning cached overview for %s)\n", sym)
		o := cachedOverview.(Overview)
		return &o, nil
	}

	log.Printf("*** Fetching overview for %s ***\n", sym)
	avKey := "G32E29AFMPQ2MCRG"
	sreq := fmt.Sprintf("https://www.alphavantage.co/query?function=OVERVIEW&symbol=%s&apikey=%s", sym, avKey)

	req, err := http.NewRequest("GET", sreq, nil)
	if err != nil {
		return nil, err
	}
	c := http.Client{}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var avo AlphaVantageOverview
	err = json.Unmarshal(bs, &avo)
	if err != nil {
		return nil, err
	}

	var o Overview
	o.Symbol = avo.Symbol
	o.AssetType = avo.AssetType
	o.Name = avo.Name
	o.Description = avo.Description
	o.Exchange = avo.Exchange

	if o.Symbol != "" {
		// Cache overview for 24 hours.
		cache.Set("overview", sym, o, 60*24)
	} else {
		// Data source returned nothing, so try fetching again after 1 hour.
		// Empty result will be cached to limit data source requests.
		cache.Set("overview", sym, o, 60)
	}

	return &o, nil
}

type AlphaVantagePrice struct {
	GlobalQuote struct {
		Symbol string `json:"01. symbol"`
		Date   string `json:"07. latest trading day"`
		Open   string `json:"02. open"`
		High   string `json:"03. high"`
		Low    string `json:"04. low"`
		Price  string `json:"05. price"`
		Volume string `json:"06. volume"`
	} `json:"Global Quote"`
}

func fetchStockPrice(sym string, cache Cache) (*Price, error) {
	cachedPrice := cache.Lookup("price", sym)
	if cachedPrice != nil {
		log.Printf("(Returning cached price for %s)\n", sym)
		p := cachedPrice.(Price)
		return &p, nil
	}

	log.Printf("*** Fetching price for %s ***\n", sym)
	avKey := "G32E29AFMPQ2MCRG"
	sreq := fmt.Sprintf("https://www.alphavantage.co/query?function=GLOBAL_QUOTE&symbol=%s&apikey=%s", sym, avKey)

	req, err := http.NewRequest("GET", sreq, nil)
	if err != nil {
		return nil, err
	}
	c := http.Client{}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var avp AlphaVantagePrice
	err = json.Unmarshal(bs, &avp)
	if err != nil {
		return nil, err
	}

	var p Price
	p.Symbol = avp.GlobalQuote.Symbol
	p.Date = avp.GlobalQuote.Date
	p.Open = atof(avp.GlobalQuote.Open)
	p.High = atof(avp.GlobalQuote.High)
	p.Low = atof(avp.GlobalQuote.Low)
	p.Price = atof(avp.GlobalQuote.Price)
	p.Volume = atof(avp.GlobalQuote.Volume)

	if p.Symbol != "" {
		// Cache price for 1 hour.
		cache.Set("price", sym, p, 60)
	} else {
		// Data source returned nothing, so try fetching again after 5 mins.
		// Empty result will be cached to limit data source requests.
		cache.Set("price", sym, p, 5)
	}

	return &p, nil
}

type GoldApiPrice struct {
	Timestamp      int64   `json:"timestamp"`
	Metal          string  `json:"metal"`
	Currency       string  `json:"currency"`
	Exchange       string  `json:"exchange"`
	Symbol         string  `json:"symbol"`
	PrevClosePrice float64 `json:"prev_close_price"`
	OpenPrice      float64 `json:"open_price"`
	LowPrice       float64 `json:"low_price"`
	HighPrice      float64 `json:"high_price"`
	Price          float64 `json:"price"`
	Ask            float64 `json:"ask"`
	Bid            float64 `json:"bid"`
}

func fetchMetalPrice(sym string, cache Cache) (*Price, error) {
	cachedPrice := cache.Lookup("price", sym)
	if cachedPrice != nil {
		log.Printf("(Returning cached price for %s)\n", sym)
		p := cachedPrice.(Price)
		return &p, nil
	}

	log.Printf("*** Fetching metal price for %s ***\n", sym)
	key := "goldapi-4g7vk3ykeikk2o0-io"
	sreq := fmt.Sprintf("https://www.goldapi.io/api/%s/USD", sym)

	req, err := http.NewRequest("GET", sreq, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-access-token", key)
	c := http.Client{}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var gldPrice GoldApiPrice
	err = json.Unmarshal(bs, &gldPrice)
	if err != nil {
		return nil, err
	}

	var p Price
	p.Symbol = gldPrice.Metal
	p.Open = gldPrice.OpenPrice
	p.High = gldPrice.HighPrice
	p.Low = gldPrice.LowPrice
	p.Price = gldPrice.Price
	tm := time.Unix(gldPrice.Timestamp, 0)
	p.Date = tm.Format(time.RFC3339)

	if p.Symbol != "" {
		// Cache price for 1 hour.
		cache.Set("price", sym, p, 60)
	} else {
		// Data source returned nothing, so try fetching again after 5 mins.
		// Empty result will be cached to limit data source requests.
		cache.Set("price", sym, p, 5)
	}

	return &p, nil
}

type Quote struct {
	Symbol string  `json:"symbol"`
	Name   string  `json:"name"`
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}
type Overview struct {
	Symbol      string `json:"symbol"`
	AssetType   string `json:"assettype"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Exchange    string `json:"exchange"`
}
type Price struct {
	Symbol string  `json:"symbol"`
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

func lookupHandler(db *sql.DB, mc, dbc Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qsym := strings.ToUpper(r.FormValue("sym"))
		if qsym == "" {
			http.Error(w, "sym required", 401)
			return
		}
		syms := strings.Split(qsym, ",")

		var qq []*Quote

		for _, sym := range syms {
			var q Quote
			if sym == "XAU" || sym == "XAG" || sym == "XPT" || sym == "XPD" || sym == "XRH" {
				price, err := fetchMetalPrice(sym, mc)
				if err != nil {
					handleErr(w, err, "fetchMetalPrice")
					return
				}

				q.Symbol = price.Symbol
				q.Date = price.Date
				q.Open = price.Open
				q.High = price.High
				q.Low = price.Low
				q.Price = price.Price
				q.Volume = price.Volume

				if sym == "XAU" {
					q.Name = "Spot Gold"
				} else if sym == "XAG" {
					q.Name = "Spot Silver"
				} else if sym == "XPT" {
					q.Name = "Spot Platinum"
				} else if sym == "XPD" {
					q.Name = "Spot Palladium"
				} else if sym == "XRH" {
					q.Name = "Spot Rhodium"
				}
			} else {
				overview, err := fetchOverview(sym, dbc)
				if err != nil {
					handleErr(w, err, "fetchOverview")
					return
				}
				price, err := fetchStockPrice(sym, mc)
				if err != nil {
					handleErr(w, err, "fetchStockPrice")
					return
				}

				q.Symbol = price.Symbol
				q.Name = overview.Name
				q.Date = price.Date
				q.Open = price.Open
				q.High = price.High
				q.Low = price.Low
				q.Price = price.Price
				q.Volume = price.Volume
			}

			if q.Symbol != "" {
				qq = append(qq, &q)
			}
		}

		bs, err := json.MarshalIndent(qq, "", "\t")
		if err != nil {
			handleErr(w, err, "lookupHandler")
			return
		}
		jsonqq := string(bs)

		w.Header().Set("Content-Type", "application/json")
		P := makeFprintf(w)
		P(jsonqq)
	}
}
