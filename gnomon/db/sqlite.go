package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"gnomon/show"
	"gnomon/structs"

	"github.com/mattn/go-sqlite3"
	_ "github.com/mattn/go-sqlite3"
)

var Mutex sync.Mutex

// copied from main
var UseMem = false
var StartAt = int64(0) // Start at Block Height, will be auto-set when using 0
type SqlStore struct {
	DB      *sql.DB
	Db_path string
	Cancel  bool
}

var dbready = true
var callers = 0

// For external use
func SetReady(r bool) {
	ready(r)
}
func ready(ready bool) {
	if UseMem {
		return
	}
	Mutex.Lock()
	if ready == false {
		callers++
	} else if ready == true {
		callers--
		if callers == 0 {
			dbready = true
		}
	}
	Mutex.Unlock()
}

func Ask() bool {
	if UseMem {
		return true
	}
	for {
		Mutex.Lock()
		if dbready {
			dbready = false
			Mutex.Unlock()
			return true
		}
		Mutex.Unlock()
	}
}
func (ss *SqlStore) WriteToDisk(end int64) error {
	ready(false)
	addon := ";"
	if end != -1 {
		addon = " AND height < " + strconv.Itoa(int(end)) + ";"
	}
	dest, err := sql.Open("sqlite3", ss.Db_path)
	if err != nil {
		ready(true)
		return fmt.Errorf("failed to open destination DB: %w", err)
	}
	defer dest.Close()

	_, err = ss.DB.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS diskdb", ss.Db_path))
	if err != nil {
		log.Fatalf("attach disk DB: %v", err)
	}

	query := ""

	var lastindexedheight int
	err = dest.QueryRow("SELECT value FROM state WHERE name = 'lastindexedheight' ").Scan(&lastindexedheight)
	height := ""
	if lastindexedheight >= 0 {
		height = strconv.Itoa(lastindexedheight)
	}
	if err != nil {
		query += "INSERT INTO diskdb.state (name,value) SELECT * FROM main.state WHERE  name = 'lastindexedheight';"
		query += "INSERT INTO diskdb.state (name,value) SELECT * FROM main.state WHERE  name = 'sessionstart';"
	} else {
		query += "UPDATE diskdb.state SET value = (SELECT value FROM main.state WHERE name = 'lastindexedheight') WHERE name = 'lastindexedheight';"
		query += "UPDATE diskdb.state SET value = (SELECT value FROM main.state WHERE name = 'sessionstart') WHERE name = 'sessionstart';"
	}

	//see if the competed record exists yet
	var checkcompleted string
	newerr := dest.QueryRow("SELECT value FROM settings WHERE name = 'completed' ").Scan(&checkcompleted)

	if newerr != nil {
		query += "INSERT INTO diskdb.settings (name,value) SELECT * FROM main.settings WHERE name = 'completed';"
	} else {
		query += "UPDATE diskdb.settings SET value = (SELECT value FROM main.settings WHERE name = 'completed') WHERE name = 'completed';"
	}

	var scs_count int
	ss.DB.QueryRow("SELECT count(*) as scs_count FROM scs").Scan(&scs_count)

	if scs_count > 0 {
		query += "INSERT INTO diskdb.scs (scs_id,scid,owner,height,scname,scdescr,scimgurl,class,tags) SELECT * FROM main.scs WHERE height >= " + height + addon

		query += "INSERT INTO diskdb.invokes (scid,signer,txid,height,entrypoint) SELECT * FROM invokes WHERE height >= " + height + addon

		query += "INSERT INTO diskdb.interactions (height,txid,scid) SELECT * FROM interactions WHERE height >= " + height + addon

		query += "INSERT INTO diskdb.variables (v_id,height,txid,vars) SELECT * FROM variables WHERE height >= " + height + addon
	}

	_, err = ss.DB.Exec(query)
	if err != nil {
		log.Printf("Last error copying tables: %v", err)
	}
	_, _ = ss.DB.Exec("DETACH DATABASE diskdb")
	ready(true)
	return err
}

func (ss *SqlStore) BackupToDisk() error {

	// Open destination database
	dest, err := sql.Open("sqlite3", ss.Db_path)
	if err != nil {
		return fmt.Errorf("failed to open destination DB: %w", err)
	}
	defer dest.Close()

	// Use the SQLite backup API
	// This requires the mattn/go-sqlite3 driver
	ctx, cancel := context.WithTimeout(context.Background(), 60000) //1 min
	defer cancel()

	connSrc, err := ss.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get source connection: %w", err)
	}

	connDest, err := dest.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get destination connection: %w", err)
	}

	defer connDest.Close()

	// Perform backup using driver-specific interface
	if err := connDest.Raw(func(destConn interface{}) error {
		return connSrc.Raw(func(srcConn interface{}) error {
			// Type assert to *sqlite3.SQLiteConn
			srcSQLite, ok1 := srcConn.(*sqlite3.SQLiteConn)
			destSQLite, ok2 := destConn.(*sqlite3.SQLiteConn)
			if !ok1 || !ok2 {
				return fmt.Errorf("unexpected connection type")
			}
			// Backup from "main" to "main"
			bk, err := destSQLite.Backup("main", srcSQLite, "main")
			if err != nil {
				return fmt.Errorf("backup init failed: %w", err)
			}
			defer bk.Finish()

			done, err := bk.Step(-1) // -1 = copy all pages
			if err != nil {
				return fmt.Errorf("backup step failed: %w", err)
			}
			if !done {
				return fmt.Errorf("backup incomplete")
			}
			return nil
		})
	}); err != nil {
		return err
	}

	return nil

}
func NewDiskDB(db_path, db_name string) (*SqlStore, error) {
	var err error
	var Sql_backend *SqlStore = &SqlStore{}

	if err := os.MkdirAll(db_path, 0700); err != nil {
		return nil, fmt.Errorf("directory creation err %s - dirpath %s", err, db_path)
	}
	full_path := filepath.Join(db_path, db_name)
	Sql_backend.DB, err = sql.Open("sqlite3", full_path)

	Sql_backend.Db_path = full_path

	return Sql_backend, err
}
func NewSqlDB(db_path, db_name string) (*SqlStore, error) {
	var err error
	var SqlBackend *SqlStore = &SqlStore{}

	if err := os.MkdirAll(db_path, 0700); err != nil {
		return nil, fmt.Errorf("directory creation err %s - dirpath %s", err, db_path)
	}
	full_path := filepath.Join(db_path, db_name)
	hard, err := sql.Open("sqlite3", full_path)
	CreateTables(hard)
	hard.Close()

	SqlBackend.DB, err = sql.Open("sqlite3", "file:diskdb?mode=memory&cache=shared")

	// Load from disk into memory
	_, err = SqlBackend.DB.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS diskdb", full_path))
	if err != nil {
		log.Fatalf("attach disk DB: %v", err)
	}
	_, err = SqlBackend.DB.Exec(

		"CREATE TABLE IF NOT EXISTS main.state AS SELECT * FROM diskdb.state;" +
			"CREATE TABLE IF NOT EXISTS main.settings (" +
			"name  TEXT PRIMARY KEY, " +
			"value  TEXT) ; " +
			"INSERT INTO settings (name,value) SELECT * FROM diskdb.settings;" +
			"CREATE TABLE IF NOT EXISTS main.scs (" +
			"scs_id INTEGER PRIMARY KEY, " +
			"scid TEXT UNIQUE NOT NULL, " +
			"owner TEXT NOT NULL, " +
			"height INTEGER, " +
			"scname TEXT, " +
			"scdescr TEXT, " +
			"scimgurl TEXT, " +
			"class TEXT, " +
			"tags TEXT);" +
			"INSERT INTO scs (scs_id,scid,owner,height,scname,scdescr,scimgurl,class,tags) SELECT * FROM diskdb.scs;" +
			"CREATE TABLE IF NOT EXISTS main.variables AS SELECT * FROM diskdb.variables;" +
			"CREATE TABLE IF NOT EXISTS main.invokes (" +
			"scid TEXT, " +
			"signer TEXT, " +
			"txid TEXT UNIQUE, " +
			"height INTEGER, " +
			"entrypoint TEXT); " +
			"INSERT INTO invokes (scid,signer,txid,height,entrypoint) SELECT * FROM diskdb.invokes;" +
			"CREATE TABLE IF NOT EXISTS main.interactions (" +
			"height INTEGER, " +
			"txid TEXT UNIQUE, " +
			"scid TEXT);" +
			"INSERT INTO interactions (height,txid,scid) SELECT * FROM diskdb.interactions;")
	if err != nil {
		log.Printf("No existing table to copy: %v", err)
	}
	_, _ = SqlBackend.DB.Exec("DETACH DATABASE diskdb")

	SqlBackend.Db_path = full_path

	return SqlBackend, err
}

func CreateTables(Db *sql.DB) {

	var startup = [6]string{}
	startup[0] = "CREATE TABLE IF NOT EXISTS state (" +
		"name  TEXT, " +
		"value  INTEGER)"

	startup[1] = "CREATE TABLE IF NOT EXISTS settings (" +
		"name  TEXT PRIMARY KEY, " +
		"value  TEXT)"

	startup[2] = "CREATE TABLE IF NOT EXISTS scs (" +
		"scs_id INTEGER PRIMARY KEY, " +
		"scid TEXT UNIQUE NOT NULL, " +
		"owner TEXT NOT NULL, " +
		"height INTEGER, " +
		"scname TEXT, " +
		"scdescr TEXT, " +
		"scimgurl TEXT, " +
		"class TEXT, " +
		"tags TEXT) "

	startup[3] = "CREATE TABLE IF NOT EXISTS variables (" +
		"v_id INTEGER PRIMARY KEY, " +
		"height INTEGER, " +
		"txid TEXT, " +
		"vars TEXT)"

	startup[4] = "CREATE TABLE IF NOT EXISTS invokes (" +
		"scid TEXT, " +
		"signer TEXT, " +
		"txid TEXT UNIQUE, " +
		"height INTEGER, " +
		"entrypoint TEXT)"

	//interactions at heightid INTEGER PRIMARY KEY
	startup[5] = "CREATE TABLE IF NOT EXISTS interactions (" +
		"height INTEGER, " +
		"txid TEXT UNIQUE, " +
		"scid TEXT)"

	for _, create := range startup {
		executeQuery(Db, create)
	}

	var exists int
	Db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='state');").Scan(&exists)
	if exists == 0 {
		fmt.Println("setting defaults")
		statement, err := Db.Prepare("CREATE INDEX height_index ON interactions(scid,txid);")
		handleError(err)
		statement.Exec()

		statement, err = Db.Prepare("CREATE INDEX invokes_height_index ON invokes(txid);")
		handleError(err)
		statement.Exec()

		/*set defaults	*/
		statement, err = Db.Prepare(`INSERT INTO scs (scid,owner) VALUES('0000000000000000000000000000000000000000000000000000000000000001','Cap''n Crunch');`)
		handleError(err)
		statement.Exec()

	}

}
func (ss *SqlStore) SaveSetting(name, value string) {
	SaveSetting(ss.DB, name, value)
}

func (ss *SqlStore) LoadSetting(name string) (value string, err error) {
	return LoadSetting(ss.DB, name)
}

func SaveSetting(Db *sql.DB, name, value string) {
	statement, err := Db.Prepare("REPLACE INTO settings (name,value) VALUES(?,?);")
	handleError(err)
	ready(false)
	statement.Exec(name, value)
	ready(true)
}

func LoadSetting(Db *sql.DB, name string) (value string, err error) {
	ready(false)
	Db.QueryRow("SELECT value FROM settings WHERE name = ?;", name).Scan(&value)
	ready(true)
	return
}

// called once when we know where we can start from
func (ss *SqlStore) SaveInitialHeight(startat int64) {
	//set defaults
	statement, err := ss.DB.Prepare("INSERT INTO state (name,value) VALUES('lastindexedheight',?);")
	handleError(err)
	ready(false)
	statement.Exec(int(startat))
	ready(true)
}

// called once when we know where we can start from
func (ss *SqlStore) SaveInitialSessionStart(startat int64) {
	//set defaults
	statement, err := ss.DB.Prepare("INSERT INTO state (name,value) VALUES('sessionstart',?);")
	handleError(err)
	ready(false)
	statement.Exec(int(startat))
	ready(true)
}

func (ss *SqlStore) LoadState(name string) (value int, err error) {
	ready(false)
	ss.DB.QueryRow("SELECT value FROM state WHERE name = ?;", name).Scan(&value)
	ready(true)
	return
}

// Stores session starting height
func (ss *SqlStore) StoreSessionStart(start int64) {
	ready(false)
	result, err := ss.DB.Exec("UPDATE state SET value = '" + strconv.Itoa(int(start)) + "' WHERE name = 'sessionstart';")

	if err == nil {
		affected_rows, _ := result.RowsAffected()
		if affected_rows != 0 {
			ready(true)
			return
		}
	} else {
		fmt.Println("Error storing session start")
	}
	ready(true)
	return
}

// Stores last indexed height - this is for stateful stores on close and reference on open
func (ss *SqlStore) StoreLastIndexHeight(last_indexedheight int64) (changes bool, err error) {
	ready(false)
	result, err := ss.DB.Exec("UPDATE state SET value = '" + strconv.Itoa(int(last_indexedheight)) + "' WHERE name = 'lastindexedheight';")

	if err == nil {
		affected_rows, _ := result.RowsAffected()
		if affected_rows != 0 {
			changes = true
			ready(true)
			return
		}
	} else {
		fmt.Println("Error storing last index height")
	}
	ready(true)
	return
}

// Gets last indexed height - this is for stateful stores on close and reference on open
func (ss *SqlStore) GetLastIndexHeight() (topoheight int64, err error) {
	var lastindexedheight int
	ready(false)
	err = ss.DB.QueryRow("SELECT value FROM state WHERE name = 'lastindexedheight' ").Scan(&lastindexedheight)
	ready(true)
	if err == nil {
		if lastindexedheight > 0 {
			topoheight = int64(lastindexedheight)
		}
		if topoheight == 0 {
			fmt.Println("[sqlite-GetLastIndexHeight] No stored last index height. Starting from 0 or latest if fastsync is enabled")
		}
	}
	return
}

// executeQuery prepares and executes a SQL query.
func executeQuery(Db *sql.DB, query string) {
	statement, err := Db.Prepare(query)
	handleError(err)
	statement.Exec()
}

// handleError logs the error if LOGGING is enabled and then exits the program.
func handleError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (ss *SqlStore) TrimHeight(start int64, end int64) int64 {
	addon := ";"
	if end != -1 {
		addon = " AND height < " + strconv.Itoa(int(end)) + ";"
	}
	ready(false)
	statement, err := ss.DB.Prepare("DELETE FROM scs WHERE height >= " + strconv.Itoa(int(start)) + addon)
	handleError(err)
	statement.Exec()
	statement, err = ss.DB.Prepare("DELETE FROM variables WHERE height >= " + strconv.Itoa(int(start)) + addon)
	handleError(err)
	statement.Exec()
	statement, err = ss.DB.Prepare("DELETE FROM invokes WHERE height >= " + strconv.Itoa(int(start)) + addon)
	handleError(err)
	statement.Exec()
	statement, err = ss.DB.Prepare("DELETE FROM interactions WHERE height >= " + strconv.Itoa(int(start)) + addon)
	handleError(err)
	statement.Exec()
	ready(true)
	return int64(start)
}

var SpamLevel = "0"
var Spammers []string

func (ss *SqlStore) RidSpam() {
	if SpamLevel == "n" {
		SpamLevel = "0"
	}
	rows, err := ss.DB.Query(`
	SELECT DISTINCT signer
	FROM invokes
	WHERE signer IN (
		SELECT signer
		FROM invokes
		GROUP BY signer	
		HAVING COUNT(signer) > `+SpamLevel+` AND invokes.scid = '0000000000000000000000000000000000000000000000000000000000000001'
		ORDER BY COUNT(signer) DESC
	)`, nil)
	if err != nil {
		fmt.Println(err)
	}
	// Just filter for the latest spammers
	Spammers := Spammers[0:0]
	var (
		spammeraddress string
	)
	for rows.Next() {
		rows.Scan(&spammeraddress)
		Spammers = append(Spammers, spammeraddress)
	}
	in := ""
	for _, spammer := range Spammers {
		in += "'" + spammer + "',"
	}
	spamaddrs := strings.TrimRight(in, ",")

	var spammerstxs []string

	rows, err = ss.DB.Query("SELECT txid FROM invokes WHERE signer IN ("+spamaddrs+") AND scid = '0000000000000000000000000000000000000000000000000000000000000001';", nil)
	if err != nil {
		fmt.Println(err)
	}
	var (
		spammertx string
	)
	for rows.Next() {
		rows.Scan(&spammertx)
		spammerstxs = append(spammerstxs, spammertx)
	}
	in = ""
	for _, spammertx := range spammerstxs {
		in += "'" + spammertx + "',"
	}
	spamtxs := strings.TrimRight(in, ",")
	show.NewMessage(show.Message{Text: "Deleting invokes:", Vars: []any{spamaddrs}})

	_, err = ss.DB.Exec("DELETE FROM invokes WHERE signer IN (" + spamaddrs + ") AND scid = '0000000000000000000000000000000000000000000000000000000000000001';")
	if err != nil {
		log.Fatal(err)
	}

	//fmt.Println("deleting vars:", spamtxs)
	_, err = ss.DB.Exec("DELETE FROM variables WHERE txid IN (" + spamtxs + ")")
	if err != nil {
		log.Fatal(err)
	}
}

// --- extras...
func (ss *SqlStore) ViewTables() {
	show.NewMessage(show.Message{Text: "Open: ", Vars: []any{ss.Db_path}})
	hard, err := sql.Open("sqlite3", ss.Db_path)
	if err != nil {
		log.Fatal(err)
	}
	defer hard.Close()
	/// check tables
	show.NewMessage(show.Message{Text: "Showing State: "})
	rows, err := hard.Query("SELECT name, value FROM state WHERE name = 'lastindexedheight'", nil)
	if err != nil {
		fmt.Println(err)
	}
	var (
		name  string
		value string
	)

	for rows.Next() {
		rows.Scan(&name, &value)
		if name == "lastindexedheight" && value == "0" {
			panic("lastindexedheight should be more than 0 probably")
		}
		show.NewMessage(show.Message{Text: "Last Indexed Height", Vars: []any{value}})
	}

	show.NewMessage(show.Message{Text: "Showing 20 Latest SCs / Owners: "})
	rows, err = hard.Query("SELECT scid, owner, scname,class, tags FROM scs WHERE class !='' ORDER BY scs_id DESC LIMIT 10", nil)
	if err != nil {
		fmt.Println(err)
	}
	var (
		scid   string
		owner  string
		scname string
		class  string
		tags   string
	)

	for rows.Next() {
		rows.Scan(&scid, &owner, &scname, &class, &tags)
		show.NewMessage(show.Message{Text: owner + "--" + scid + "--" + scname + "--" + class + "--" + tags})
	}
	show.NewMessage(show.Message{Text: "Showing Vars: "})
	rows, err = hard.Query("SELECT count(*) FROM variables", nil)
	if err != nil {
		fmt.Println(err)
	}
	var (
		vcount int
	)
	for rows.Next() {
		rows.Scan(&vcount)
		show.NewMessage(show.Message{Text: "Count: ", Vars: []any{vcount}})
	}

	show.NewMessage(show.Message{Text: "Showing Interactions: "})
	rows, err = hard.Query("SELECT count(*) FROM interactions", nil)
	if err != nil {
		fmt.Println(err)
	}
	var (
		count string
	)
	for rows.Next() {
		rows.Scan(&count)
		show.NewMessage(show.Message{Text: "Count: ", Vars: []any{count}})
	}

	/*


			SELECT DISTINCT scid
		FROM invokes
		WHERE scid IN (
			SELECT scid
			FROM invokes
			GROUP BY scid
			HAVING COUNT(scid) > 10000
			ORDER BY COUNT(scid) DESC
			)
			SELECT scid FROM interactions
			GROUP BY scid
			HAVING COUNT(*) = (
			                   SELECT MAX(Cnt)
			                   FROM(
			                         SELECT COUNT(*) as Cnt
			                         FROM interactions
			                         GROUP BY scid
			                        ) tmp
			                    )
	*/
}

func (ss *SqlStore) GetSCIDsByClass(class_list []string) (results []string) {

	qinsert := ""
	var qvars = []any{}
	for _, class := range class_list {
		qinsert += "OR (class = ?) OR (? LIKE (class || '%')) OR (? LIKE ('%' || class || '%')) OR (? LIKE ('%' || class)) "
		qvars = append(
			qvars,
			class,
			class+",",
			","+class+",",
			","+class,
		)
	}
	qinsert = strings.TrimPrefix(qinsert, "OR ")
	ready(false)
	rows, err := ss.DB.Query("SELECT scid FROM scs WHERE "+qinsert, qvars...)
	if err != nil {
		fmt.Println(err)
	}
	ready(true)
	var scid string
	for rows.Next() {
		rows.Scan(&scid)
		results = append(results, scid)
	}
	return results
}

func (ss *SqlStore) GetSCIDsByTags(tags_list []string) (results []string) {
	qinsert := ""
	var qvars = []any{}
	for _, tag := range tags_list {
		qinsert += "OR (tags = ?) OR (? LIKE (tags || '%')) OR (? LIKE ('%' || tags || '%')) OR (? LIKE ('%' || tags)) " //consider prepared statement
		qvars = append(
			qvars,
			tag,
			tag+",",
			","+tag+",",
			","+tag,
		)
	}
	qinsert = strings.TrimPrefix(qinsert, "OR ")
	ready(false)
	rows, err := ss.DB.Query("SELECT scid FROM scs WHERE "+qinsert, qvars...)
	if err != nil {
		fmt.Println(err)
	}
	ready(true)
	var (
		scid string
	)
	for rows.Next() {
		rows.Scan(&scid)
		r := scid
		results = append(results, r)
	}
	return results
}

func (ss *SqlStore) GetSCsByTags(tags_list []string) (results []map[string]any) {
	qinsert := ""
	var qvars = []any{}
	for _, tag := range tags_list {
		qinsert += "OR (tags = ?) OR (? LIKE (tags || '%')) OR (? LIKE ('%' || tags || '%')) OR (? LIKE ('%' || tags)) "
		qvars = append(
			qvars,
			tag,
			tag+",",
			","+tag+",",
			","+tag,
		)
	}
	qinsert = strings.TrimPrefix(qinsert, "OR ")
	ready(false)
	rows, err := ss.DB.Query("SELECT scid,owner,height,scname,scdescr,scimgurl,class,tags FROM scs WHERE "+qinsert, qvars...)
	if err != nil {
		fmt.Println(err)
	}
	ready(true)
	var (
		scid     string
		owner    string
		height   int
		scname   string
		scdescr  string
		scimgurl string
		class    string
		tags     string
	)

	for rows.Next() {
		rows.Scan(&scid, &owner, &height, &scname, &scdescr, &scimgurl, &class, &tags)
		r := map[string]any{
			"scid":     scid,
			"owner":    owner,
			"height":   height,
			"scname":   scname,
			"scdescr":  scdescr,
			"scimgurl": scimgurl,
			"class":    class,
			"tags":     tags,
		}
		results = append(results, r)
	}
	return results
}

// Gets SC variables at a given topoheight
func (ss *SqlStore) UpdateSCMeta(scid, class, tags string) {
	ready(false) //not really need right now
	result, err := ss.DB.Exec(
		`UPDATE scs
		SET class = ?, tags = ?
		WHERE scid = ?;
		`, class, tags, scid)
	if err == nil {
		affected_rows, _ := result.RowsAffected()
		if affected_rows != 0 {
			ready(true)
			return
		}
	} else {
		fmt.Println("Error updating metadata")
	}
	ready(true)
	return
}

// Gets SCs
func (ss *SqlStore) GetSCIDS() (results []string) {

	ready(false)
	rows, _ := ss.DB.Query(
		`SELECT height,scid
		FROM scs
		GROUP BY height
		ORDER BY height ASC;`)
	ready(true)
	var (
		height int
		scid   string
	)
	for rows.Next() {
		rows.Scan(&height, &scid)
		results = append(results, scid)
	}
	return
}

// Get sc code from the install tx
func (ss *SqlStore) GetInitialSCIDCode(scid string) (sc_code string, err error) {
	var height int
	var vars string
	ready(false)
	err = ss.DB.QueryRow("SELECT height,vars FROM variables WHERE txid = ? ", scid).Scan(&height, &vars)
	ready(true)
	var heights []int64
	results := make(map[int64][]*structs.SCIDVariable)
	var variables []*structs.SCIDVariable
	_ = json.Unmarshal([]byte(vars), &variables)
	topoheight, _ := strconv.ParseInt(string(height), 10, 64)
	results[topoheight] = variables
	heights = append(heights, topoheight)

	if results != nil {
		variables = getTypedVariables(heights, results)
	}
	for _, v := range variables {
		if v.Key == "C" {
			sc_code = v.Value.(string)
		}
	}
	return
}

// -----------------
// -----------------
// -----------------
// -----------------

// Stores the owner (who deployed it) of a given scid
func (ss *SqlStore) StoreOwner(scid string, owner string, height int, scname string, scdescr string, scimgurl string, class string, tags string) (changes bool, err error) {
	if ss.Cancel {
		return
	}
	ready(false)
	statement, err := ss.DB.Prepare("INSERT INTO scs (scid,owner,height,scname,scdescr,scimgurl,class,tags) VALUES (?,?,?,?,?,?,?,?)")
	if err != nil {
		log.Fatal(err)
	}

	result, err := statement.Exec(
		scid,
		owner,
		height,
		scname,
		scdescr,
		scimgurl,
		class,
		tags,
	)

	if err == nil {
		last_insert_id, _ := result.LastInsertId()
		//	fmt.Println("ownerinsertid: ", last_insert_id)
		if last_insert_id >= 0 {
			changes = true
			ready(true)
			return
		}

	} else {
		ss.Cancel = true
	}
	ready(true)
	return

}

// Returns all of the deployed SCIDs with their corresponding owners (who deployed it)
func (ss *SqlStore) GetAllOwnersAndSCIDs() map[string]string {
	results := make(map[string]string)
	ready(false)
	rows, _ := ss.DB.Query("SELECT scid, owner FROM scs", nil)
	ready(true)
	var (
		scid  string
		owner string
	)

	for rows.Next() {
		rows.Scan(&scid, &owner)
		results[scid] = owner
	}
	return results

}

// Stores SC variables at a given topoheight (called on any new scdeploy or scinvoke actions)
func (ss *SqlStore) StoreSCIDVariableDetails(scid string, variables []*structs.SCIDVariable, topoheight int64) (changes bool, err error) {
	if ss.Cancel {
		return
	}
	confBytes, err := json.Marshal(variables)
	if err != nil {
		return changes, fmt.Errorf("[StoreSCIDVariableDetails] could not marshal getinfo info: %v", err)
	}

	ready(false) //maybe look up the scid id
	statement, err := ss.DB.Prepare("INSERT INTO variables (height, txid, vars) VALUES (?,?,?)")

	if err != nil {
		log.Fatal(err)
	}

	result, err := statement.Exec(
		int(topoheight),
		scid,
		confBytes,
	)

	if err == nil {
		last_insert_id, _ := result.LastInsertId()
		if last_insert_id >= 0 {
			changes = true
			ready(true)
			return
		}
	} else {
		ss.Cancel = true
	}
	ready(true)
	return
}

// Gets SC variables at a given topoheight
func (ss *SqlStore) GetSCIDVariableDetailsAtTopoheight(scid string, topoheight int64) (hVars []*structs.SCIDVariable) {
	results := make(map[int64][]*structs.SCIDVariable)
	var heights []int64

	ready(false)

	rows, _ := ss.DB.Query(
		`SELECT height, vars
			FROM variables
			WHERE txid IN (
				SELECT txid
				FROM invokes
				WHERE scid = ?
			) OR txid IN (
				SELECT scid
				FROM scs
				WHERE scid = ?
			) AND height <= ? ORDER BY height ASC;`,
		scid,
		scid,
		int(topoheight),
	)
	ready(true)
	var (
		vars   string
		height int
	)

	for rows.Next() {
		rows.Scan(&height, &vars)
		topoheight, _ := strconv.ParseInt(string(height), 10, 64)
		heights = append(heights, topoheight)
		var variables []*structs.SCIDVariable
		_ = json.Unmarshal([]byte(vars), &variables)
		results[topoheight] = variables
	}

	if results != nil {
		hVars = getTypedVariables(heights, results)
	}

	return
}

// Function not needed for indexer...
// Gets SC variables at all topoheights
func (ss *SqlStore) GetAllSCIDVariableDetails(scid string) (hVars []*structs.SCIDVariable) {
	results := make(map[int64][]*structs.SCIDVariable)
	var heights []int64
	//fmt.Println("GetAllSCIDVariableDetails", bName)
	ready(false)
	rows, _ := ss.DB.Query("SELECT height,vars FROM variables WHERE txid =? ORDER BY height ASC",
		scid,
	)
	ready(true)
	var (
		vars   string
		height int
	)

	for rows.Next() {
		rows.Scan(&height, &vars)
		topoheight, _ := strconv.ParseInt(string(height), 10, 64)
		heights = append(heights, topoheight)
		var variables []*structs.SCIDVariable
		_ = json.Unmarshal([]byte(vars), &variables)
		results[topoheight] = variables
	}

	//fmt.Println("results: ", results)

	if results != nil {
		hVars = getTypedVariables(heights, results)
	}

	return
}

// Stores SC interaction height and detail - height invoked upon and type (scinstall/scinvoke). This is separate tree & k/v since we can query it for other things at less data retrieval
func (ss *SqlStore) StoreSCIDInvoke(scidstoadd structs.SCIDToIndexStage, height int64) (changes bool, err error) {

	//	fmt.Println("\nStoreSCIDInvoke... TXHash " + scidstoadd.TXHash + " ParamsSCID " + scidstoadd.Params.SCID + " Height:" + strconv.Itoa(int(height)))

	ready(false)
	statement, err := ss.DB.Prepare("INSERT INTO invokes (scid,signer,txid,height,entrypoint) VALUES (?,?,?,?,?);")
	if err == nil {

		result, err := statement.Exec(
			scidstoadd.Params.SCID,
			scidstoadd.Fsi.Signer,
			scidstoadd.TXHash,
			height,
			scidstoadd.Entrypoint,
		)

		if err == nil {
			last_insert_id, _ := result.LastInsertId()
			if last_insert_id >= 0 {
				//fmt.Println("\n INSERTED NEW RECORD " + strconv.Itoa(int(last_insert_id)) + " H:" + strconv.Itoa(int(height)))
				changes = true
			}
		}
	}
	ready(true)
	return

}

// Stores SC interaction height and detail - height invoked upon and type (scinstall/scinvoke). This is separate tree & k/v since we can query it for other things at less data retrieval
func (ss *SqlStore) StoreSCIDInteractionHeight(scidstoadd structs.SCIDToIndexStage, height int64) (changes bool, err error) {

	//fmt.Println("\nStoreSCIDInteractionHeight... TXHash " + scidstoadd.TXHash + " ParamsSCID " + scidstoadd.Params.SCID + " Height:" + strconv.Itoa(int(height)))

	ready(false)
	var scs_id int
	if scidstoadd.Type == "install" {
		//it is a SC install and already saved
		ready(true)
		return
	}
	statement, err := ss.DB.Prepare("INSERT INTO interactions (height,txid,scid) VALUES (?,?,?);")
	if err != nil {
		log.Fatal(err)
	}
	result, err := statement.Exec(
		height,
		scidstoadd.TXHash,
		scs_id,
	)
	if err == nil {
		last_insert_id, _ := result.LastInsertId()
		if last_insert_id >= 0 {
			//	fmt.Println("\n INSERTED NEW RECORD " + strconv.Itoa(int(last_insert_id)) + " H:" + strconv.Itoa(int(height)))
			changes = true
		}
	}
	ready(true)
	return

}

// Gets SC interaction height and detail by a given SCID
func (ss *SqlStore) GetSCIDInteractionHeight(scid string) (scidinteractions []int64) {
	//	fmt.Println("GetSCIDInteractionHeight... ")"SELECT interaction_heights.height FROM interactions INNER JOIN interactions.i_id ON interaction_heights WHERE scid=?"

	ready(false)
	rows, err := ss.DB.Query(
		"SELECT height FROM interactions WHERE txid=?",
		scid)

	if err != nil {
		fmt.Println(err)
	}
	var (
		height int
	)
	for rows.Next() {
		rows.Scan(&height)
		//	fmt.Println("height ", height)
		scidinteractions = append(scidinteractions, int64(height))
	}

	ready(true)

	return

}

// Gets SC interaction height and detail by a given SCID (maybe add to api)
func (ss *SqlStore) GetSCIDVariableHeight(scid string, rmax bool) (scidinteractions int64) {
	//	fmt.Println("GetSCIDInteractionHeight... ")"SELECT interaction_heights.height FROM interactions INNER JOIN interactions.i_id ON interaction_heights WHERE scid=?"
	sort := "DESC"
	if !rmax {
		sort = "ASC"
	}
	ready(false)
	rows, _ := ss.DB.Query(
		`SELECT height
		FROM variables
		WHERE txid IN (
			SELECT txid
			FROM invokes
			WHERE scid = ?
		) OR txid IN (
			SELECT scid
			FROM scs
			WHERE scid = ?
		) ORDER BY height `+sort+" LIMIT 1",
		scid,
		scid,
	)
	ready(true)
	var (
		height int
	)

	for rows.Next() {
		rows.Scan(&height)
		//println("height ", height)
		return int64(height)
	}

	return

}

/*
func (ss *SqlStore) GetInteractionIndex(topoheight int64, heights []int64) (height int64) {
	if len(heights) <= 0 {
		return height
	}

	// Sort heights so most recent is index 0 [if preferred reverse, just swap > with <]
	sort.SliceStable(heights, func(i, j int) bool {
		return heights[i] > heights[j]
	})

	if topoheight > heights[0] || rmax {
		return heights[0]
	}

	for i := 1; i < len(heights); i++ {
		if heights[i] < topoheight {
			return heights[i]
		} else if heights[i] == topoheight {
			return heights[i]
		}
	}

	return height
}

*/

// Gets SC values by key at given topoheight who's key equates to a given interface{} (string/uint64)
func (ss *SqlStore) GetSCIDValuesByKey(scid string, key interface{}, height int64, rmax bool) (valuesstring []string, valuesuint64 []uint64) {
	// TODO: If there's no interaction height, do we go get scvars against daemon and store? Or do we just ignore and return nil
	variables := ss.GetSCIDVariableDetailsAtTopoheight(scid, ss.GetSCIDVariableHeight(scid, rmax))
	// Switch against the value passed. If it's a uint64 or string
	return getTyped(key, variables, "keys")
}

// Gets SC variable keys at given topoheight who's value equates to a given interface{} (string/uint64)
func (ss *SqlStore) GetSCIDKeysByValue(scid string, val interface{}, height int64, rmax bool) (keysstring []string, keysuint64 []uint64) {
	// TODO: If there's no interaction height, do we go get scvars against daemon and store? Or do we just ignore and return nil
	variables := ss.GetSCIDVariableDetailsAtTopoheight(scid, ss.GetSCIDVariableHeight(scid, rmax))
	// Switch against the value passed. If it's a uint64 or string
	return getTyped(val, variables, "vars")
}

// SC typing/value extraction fucntions
func getTyped(entity interface{}, variables []*structs.SCIDVariable, gettype string) (strings []string, uint64s []uint64) {
	switch inpvar := entity.(type) {
	case uint64:
		for _, v := range variables {
			var find any
			var use any
			if gettype == "keys" {
				find = v.Key
				use = v.Value
			} else {
				find = v.Value
				use = v.Key
			}
			switch ckey := find.(type) {
			case float64:
				if inpvar == uint64(ckey) {
					switch cval := use.(type) {
					case float64:
						uint64s = append(uint64s, uint64(cval))
					case uint64:
						uint64s = append(uint64s, cval)
					default:
						// default just store as string. Keys should only ever be strings or uint64, however, but assume default to string
						strings = append(strings, use.(string))
					}
				}
			case uint64:
				if inpvar == ckey {
					switch cval := use.(type) {
					case float64:
						uint64s = append(uint64s, uint64(cval))
					case uint64:
						uint64s = append(uint64s, cval)
					default:
						// default just store as string. Keys should only ever be strings or uint64, however, but assume default to string
						strings = append(strings, use.(string))
					}
				}
			default:
				// Nothing - expect only string/uint64 for value types
			}
		}
	case string:
		for _, v := range variables {
			var find any
			var use any
			if gettype == "keys" {
				find = v.Key
				use = v.Value
			} else {
				find = v.Value
				use = v.Key
			}
			switch ckey := find.(type) {
			case string:
				if inpvar == ckey {
					switch cval := use.(type) {
					case float64:
						uint64s = append(uint64s, uint64(cval))
					case uint64:
						uint64s = append(uint64s, cval)
					default:
						// default just store as string. Values should only ever be strings or uint64, however, but assume default to string
						strings = append(strings, use.(string))
					}
				}
			default:
				// Nothing - expect only string/uint64 for value types
			}
		}
	default:
		// Nothing - expect only string/uint64 for value types
	}
	return strings, uint64s
}

func getTypedVariables(heights []int64, results map[int64][]*structs.SCIDVariable) (hVars []*structs.SCIDVariable) {
	var vs2k = make(map[interface{}]interface{})
	for _, v := range heights {
		for _, vs := range results[v] {
			switch ckey := vs.Key.(type) {
			case float64:
				switch cval := vs.Value.(type) {
				case float64:
					vs2k[uint64(ckey)] = uint64(cval)
				case uint64:
					vs2k[uint64(ckey)] = cval
				case string:
					vs2k[uint64(ckey)] = cval
				default:
					if cval != nil {
						fmt.Printf("[GetAllSCIDVariableDetails] Value '%v' does not match string, uint64 or float64.", cval)
					} else {
						vs2k[uint64(ckey)] = cval
					}
				}
			case uint64:
				switch cval := vs.Value.(type) {
				case float64:
					vs2k[ckey] = uint64(cval)
				case uint64:
					vs2k[ckey] = cval
				case string:
					vs2k[ckey] = cval
				default:
					if cval != nil {
						fmt.Printf("[GetAllSCIDVariableDetails] Value '%v' does not match string, uint64 or float64.", cval)
					} else {
						vs2k[ckey] = cval
					}
				}
			case string:
				switch cval := vs.Value.(type) {
				case float64:
					vs2k[ckey] = uint64(cval)
				case uint64:
					vs2k[ckey] = cval
				case string:
					vs2k[ckey] = cval
				default:
					if cval != nil {
						fmt.Printf("[GetAllSCIDVariableDetails] Value '%v' does not match string, uint64 or float64.", cval)
					} else {
						vs2k[ckey] = cval
					}
				}
			default:
				if ckey != nil {
					fmt.Printf("[GetAllSCIDVariableDetails] Key '%v' does not match string, uint64 or float64.", ckey)
				}
			}
		}
	}
	for k, v := range vs2k {
		// If value is nil, no reason to add.
		if v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil()) || k == nil || (reflect.ValueOf(k).Kind() == reflect.Ptr && reflect.ValueOf(k).IsNil()) {
			//logger.Debugf("[GetSCIDVariableDetailsAtTopoheight] Value '%v' or Key '%v' is nil. Continuing.", fmt.Sprintf("%v", v), fmt.Sprintf("%v", k))
			continue
		}
		co := &structs.SCIDVariable{}

		switch ckey := k.(type) {
		case float64:
			switch cval := v.(type) {
			case float64:
				co.Key = uint64(ckey)
				co.Value = uint64(cval)
			case uint64:
				co.Key = uint64(ckey)
				co.Value = cval
			case string:
				co.Key = uint64(ckey)
				co.Value = cval
			default:
				fmt.Printf("[GetSCIDVariableDetailsAtTopoheight] Value '%v' or Key '%v' does not match string, uint64 or float64.", fmt.Sprintf("%v", cval), fmt.Sprintf("%v", uint64(ckey)))
				continue
			}
		case uint64:
			switch cval := v.(type) {
			case float64:
				co.Key = ckey
				co.Value = uint64(cval)
			case uint64:
				co.Key = ckey
				co.Value = cval
			case string:
				co.Key = ckey
				co.Value = cval
			default:
				fmt.Printf("[GetSCIDVariableDetailsAtTopoheight] Value '%v' or Key '%v' does not match string, uint64 or float64.", fmt.Sprintf("%v", cval), fmt.Sprintf("%v", ckey))
				continue
			}
		case string:
			switch cval := v.(type) {
			case float64:
				co.Key = ckey
				co.Value = uint64(cval)
			case uint64:
				co.Key = ckey
				co.Value = cval
			case string:
				co.Key = ckey
				co.Value = cval
			default:
				fmt.Printf("[GetSCIDVariableDetailsAtTopoheight] Value '%v' or Key '%v' does not match string, uint64 or float64.", fmt.Sprintf("%v", cval), fmt.Sprintf("%v", ckey))
				continue
			}
		}

		hVars = append(hVars, co)
	}
	return hVars
}

/*
// Stores any SCIDs that were attempted to be deployed but not correct - log scid/fees burnt attempting it.
func (ss *SqlStore) StoreInvalidSCIDDeploys(scid string, fee uint64) (changes bool, err error) {
	var currSCIDInteractionHeight []byte

	currInvalidSCIDs := make(map[string]uint64)
	var newInvalidSCIDs []byte

	bName := "invalidscids"
	key := "invalid"

	err = ss.DB.View(func(tx *bolt.Tx) (err error) {
		b := tx.Bucket([]byte(bName))
		if b != nil {
			currSCIDInteractionHeight = b.Get([]byte(key))
		}
		return
	})

	err = ss.DB.Update(func(tx *bolt.Tx) (err error) {
		b, err := tx.CreateBucketIfNotExists([]byte(bName))
		if err != nil {
			return fmt.Errorf("bucket: %s", err)
		}

		if currSCIDInteractionHeight == nil {
			currInvalidSCIDs[scid] = fee
		} else {
			// Retrieve value and convert to SCIDInteractionHeight, so that you can manipulate and update db
			_ = json.Unmarshal(currSCIDInteractionHeight, &currInvalidSCIDs)

			currInvalidSCIDs[scid] = fee
		}
		newInvalidSCIDs, err = json.Marshal(currInvalidSCIDs)
		if err != nil {
			return fmt.Errorf("[bbs-StoreInvalidSCIDDeploys] could not marshal interactionHeight info: %v", err)
		}

		err = b.Put([]byte(key), newInvalidSCIDs)
		changes = true
		return
	})

	return
}

// Gets any SCIDs that were attempted to be deployed but not correct and their fees
func (ss *SqlStore) GetInvalidSCIDDeploys() map[string]uint64 {
	invalidSCIDs := make(map[string]uint64)

	bName := "invalidscids"

	ss.DB.View(func(tx *bolt.Tx) (err error) {
		b := tx.Bucket([]byte(bName))
		if b != nil {
			key := "invalid"
			v := b.Get([]byte(key))

			if v != nil {
				_ = json.Unmarshal(v, &invalidSCIDs)
			}
		}
		return
	})

	return invalidSCIDs
}

// Stores counts of miniblock finders by address
func (ss *SqlStore) StoreMiniblockCountByAddress(addr string) (changes bool, err error) {
	currCount := ss.GetMiniblockCountByAddress(addr)

	// Add 1 to currCount
	currCount++

	confBytes, err := json.Marshal(currCount)
	if err != nil {
		return changes, fmt.Errorf("[StoreMiniblockCountByAddress] could not marshal getinfo info: %v", err)
	}

	bName := "blockcount"

	key := addr

	err = ss.DB.Update(func(tx *bolt.Tx) (err error) {
		b, err := tx.CreateBucketIfNotExists([]byte(bName))
		if err != nil {
			return fmt.Errorf("bucket: %s", err)
		}

		err = b.Put([]byte(key), confBytes)
		changes = true
		return
	})

	return
}

// Gets counts of miniblock finders by address
func (ss *SqlStore) GetMiniblockCountByAddress(addr string) (miniblocks int64) {
	bName := "blockcount"

	ss.DB.View(func(tx *bolt.Tx) (err error) {
		b := tx.Bucket([]byte(bName))
		if b != nil {
			key := addr
			v := b.Get([]byte(key))

			if v != nil {
				_ = json.Unmarshal(v, &miniblocks)
			}
		}
		return
	})

	return
}

// Stores the integrator addrs who submit blocks
func (ss *SqlStore) StoreIntegrators(integrator string) (changes bool, err error) {
	bName := "integrators"
	key := "integrators"

	var currIntegrators []byte
	newIntegratorsStag := make(map[string]int64)
	var newIntegrators []byte

	err = ss.DB.View(func(tx *bolt.Tx) (err error) {
		b := tx.Bucket([]byte(bName))
		if b != nil {
			currIntegrators = b.Get([]byte(key))
		}
		return
	})

	err = ss.DB.Update(func(tx *bolt.Tx) (err error) {
		b, err := tx.CreateBucketIfNotExists([]byte(bName))
		if err != nil {
			return fmt.Errorf("bucket: %s", err)
		}

		if currIntegrators == nil {
			newIntegratorsStag[integrator]++
		} else {
			// Retrieve value and convert to SCIDInteractionHeight, so that you can manipulate and update db
			_ = json.Unmarshal(currIntegrators, &newIntegratorsStag)

			newIntegratorsStag[integrator]++
		}

		newIntegrators, err = json.Marshal(newIntegratorsStag)
		if err != nil {
			return fmt.Errorf("[bbs-StoreInvalidSCIDDeploys] could not marshal integrators info: %v", err)
		}

		err = b.Put([]byte(key), newIntegrators)
		changes = true
		return
	})

	return
}

// Gets integrators and their counts
func (ss *SqlStore) GetIntegrators() (integrators map[string]int64) {
	bName := "integrators"
	key := "integrators"

	ss.DB.View(func(tx *bolt.Tx) (err error) {
		b := tx.Bucket([]byte(bName))
		if b != nil {
			v := b.Get([]byte(key))

			if v != nil {
				_ = json.Unmarshal(v, &integrators)
			}
		}
		return
	})

	return
}
*/
