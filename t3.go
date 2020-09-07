package main

import (
	"database/sql"
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

type AlphaVantageOverview struct {
	Symbol      string `json:"Symbol"`
	AssetType   string `json:"AssetType"`
	Name        string `json:"Name"`
	Description string `json:"Description"`
	Exchange    string `json:"Exchange"`
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

type CacheEntry struct {
	lastMod time.Time
	item    interface{}
}
type CacheMap map[string]*CacheEntry

var _cacheQuote CacheMap
var _cacheOverview CacheMap
var _cachePrice CacheMap

func init() {
	_cacheOverview = CacheMap{}
	_cachePrice = CacheMap{}
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

func main() {
	os.Args = os.Args[1:]
	sw, parms := parseArgs(os.Args)

	// [-i new_file]  Create and initialize db file
	if sw["i"] != "" {
		dbfile := sw["i"]
		if fileExists(dbfile) {
			s := fmt.Sprintf("File '%s' already exists. Can't initialize it.\n", dbfile)
			fmt.Printf(s)
			os.Exit(1)
		}
		createTables(dbfile)
		os.Exit(0)
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
		os.Exit(0)
	}

	// Exit if db file doesn't exist.
	dbfile := parms[0]
	if !fileExists(dbfile) {
		s := fmt.Sprintf(`Sites database file '%s' doesn't exist. Create one using:
	wb -i <notes.db>
`, dbfile)
		fmt.Printf(s)
		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", dbfile, err)
		os.Exit(1)
	}

	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./static/coffee.ico") })
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	http.Handle("/", http.StripPrefix("/", http.FileServer(http.Dir("./"))))
	http.HandleFunc("/api/lookup/", lookupHandler(db))

	port := "8000"
	fmt.Printf("Listening on %s...\n", port)
	err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	log.Fatal(err)
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
func lookupCache(cmap CacheMap, id string, ttlMins int) interface{} {
	entry := cmap[id]
	if entry != nil {
		// Return cached item if it hasn't elapsed yet.
		// Elapsed is determined by ttlMins (time to live number of minutes)
		elapsedMins := time.Since(entry.lastMod) / time.Minute
		if elapsedMins < time.Duration(ttlMins) {
			return entry.item
		}
	}
	return nil
}
func setCacheItem(cmap CacheMap, id string, item interface{}) {
	cmap[id] = &CacheEntry{lastMod: time.Now(), item: item}
}
func removeCacheItem(cmap CacheMap, id string) {
	delete(cmap, id)
}

func fetchOverview(sym string) (*Overview, error) {
	cachedOverview := lookupCache(_cacheOverview, sym, 60*24)
	if cachedOverview != nil {
		log.Printf("(Returning cached overview)\n")
		return cachedOverview.(*Overview), nil
	}

	log.Printf("*** Fetching overview ***\n")
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

	setCacheItem(_cacheOverview, sym, &o)

	return &o, nil
}

func fetchPrice(sym string) (*Price, error) {
	cachedPrice := lookupCache(_cachePrice, sym, 60)
	if cachedPrice != nil {
		log.Printf("(Returning cached price)\n")
		return cachedPrice.(*Price), nil
	}

	log.Printf("*** Fetching price ***\n")
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

	setCacheItem(_cachePrice, sym, &p)

	return &p, nil
}

func fetchMetalPrice(sym string) (*Price, error) {
	cachedPrice := lookupCache(_cachePrice, sym, 60)
	if cachedPrice != nil {
		log.Printf("(Returning cached price)\n")
		return cachedPrice.(*Price), nil
	}

	log.Printf("*** Fetching metal price ***\n")
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

	setCacheItem(_cachePrice, sym, &p)

	return &p, nil
}

func lookupHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		sym := strings.ToUpper(r.FormValue("sym"))
		if sym == "" {
			http.Error(w, "sym required", 401)
			return
		}

		var quote Quote

		if sym == "XAU" || sym == "XAG" || sym == "XPT" || sym == "XPD" || sym == "XRH" {
			price, err := fetchMetalPrice(sym)
			if err != nil {
				handleErr(w, err, "fetchMetalPrice")
				return
			}

			quote.Symbol = price.Symbol
			quote.Date = price.Date
			quote.Open = price.Open
			quote.High = price.High
			quote.Low = price.Low
			quote.Price = price.Price
			quote.Volume = price.Volume

			if sym == "XAU" {
				quote.Name = "Spot Gold"
			} else if sym == "XAG" {
				quote.Name = "Spot Silver"
			} else if sym == "XPT" {
				quote.Name = "Spot Platinum"
			} else if sym == "XPD" {
				quote.Name = "Spot Palladium"
			} else if sym == "XRH" {
				quote.Name = "Spot Rhodium"
			}
		} else {
			overview, err := fetchOverview(sym)
			if err != nil {
				handleErr(w, err, "fetchOverview")
				return
			}
			price, err := fetchPrice(sym)
			if err != nil {
				handleErr(w, err, "fetchPrice")
				return
			}

			quote.Symbol = price.Symbol
			quote.Name = overview.Name
			quote.Date = price.Date
			quote.Open = price.Open
			quote.High = price.High
			quote.Low = price.Low
			quote.Price = price.Price
			quote.Volume = price.Volume
		}

		bs, err := json.MarshalIndent(quote, "", "\t")
		if err != nil {
			handleErr(w, err, "lookupHandler")
			return
		}
		jsonQuote := string(bs)

		w.Header().Set("Content-Type", "application/json")
		P := makeFprintf(w)
		P(jsonQuote)
	}
}
