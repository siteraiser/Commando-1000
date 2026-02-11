package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"gnomon"
	"gnomon/daemon"
	sql "gnomon/db"
	"gnomon/show"
	"gnomon/structs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/walletapi"
	"github.com/deroproject/derohe/walletapi/rpcserver"
	"github.com/deroproject/derohe/walletapi/xswd"
)

/*
add gnomon total mute
*/
type Dero struct {
	Wallet     *walletapi.Wallet_Disk
	Path       string
	WalletName string
	PassHash   [32]byte
	RPC        *rpcserver.RPCServer
	XSWD       *xswd.XSWD
	DaemonAddr string
}

var dero Dero

func main() {
	println(" ---------------")
	println("| Commando 1000 |")
	println(" ---------------")
	print("Initializing table...")
	initializeWalletTable()
	print("globals...\n")
	initializeGlobals()

	println("Begin input...")
	// Create a channel to receive key events
	KeyEvents = make(chan string)
	//Start listening
	go input()
	// Catch the messges and inputs
	for {
		select {
		case kinput := <-KeyEvents:
			handleKeyInput(kinput)
		case gmsg := <-show.Events:
			if updates_enabled && gnomon_updates_enabled && !gnomon_updates_muted {
				fmt.Println("Gnomon", gmsg)
			}
		}
	}
}

/* Another way to access the db
db_name := fmt.Sprintf("sql%s.db", "GNOMON")
db_path := filepath.Join(GConfig.CmdFlags["mode"].(string), "gnomondb")
Sqlite, _ = sql.NewDiskDB(db_path, db_name)
*/

var updating = false
var updates_enabled = true
var gnomon_updates_enabled = true
var gnomon_updates_muted = false

func updater() {
	if updating {
		return
	}
	updating = true
	for {
		time.Sleep(2 * time.Second)
		showInfo()
	}
}

var lastview = uint64(0)

func showInfo() {
	if dero.Wallet != nil {
		bal, _ := dero.Wallet.Get_Balance()
		h := dero.Wallet.Get_Height()
		Mutex.Lock()
		if updates_enabled && lastview != (h+bal) {
			lastview = h + bal //close enough
			bal := globals.FormatMoney(bal)
			fmt.Print("\0337")
			fmt.Print("\033[F\033[2K")
			fmt.Print("Height:", h, " Balance:", bal, ">")
			fmt.Print("\0338")
		}
		Mutex.Unlock()
	}
}

//var SuccessfulRegs chan *transaction.Transaction

// Create a channel for keyboard inputs
var KeyEvents chan string
var keyInput string
var inputmsg = ""
var pending_command = "" //

func handleKeyInput(text string) {
	text = handleSession(text)
	action(text)
}

//var inputReader *bufio.Reader

func input() {
	msg := `Type help for a list of commands`
	if inputmsg != "" {
		msg = inputmsg
	}
	fmt.Println(msg)
	Mutex.Lock()
	inputReader := bufio.NewReader(os.Stdin)
	Mutex.Unlock()
	text, err := inputReader.ReadString('\n')
	if err != nil {
		println("Error reading input:", err)
	}
	text = strings.TrimSpace(text)
	Mutex.Lock()
	inputReader.Reset(os.Stdin)
	Mutex.Unlock()
	showInfo()
	//	go input(user_message)
	KeyEvents <- text
}

var Mutex sync.Mutex

func getText(message string) string {
	Mutex.Lock()
	updates_enabled = false
	Mutex.Unlock()
	fmt.Println(message)
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		println("Error reading input:", err)
	}
	text = strings.TrimSpace(text)
	reader.Reset(os.Stdin)

	Mutex.Lock()
	updates_enabled = true
	defer Mutex.Unlock()

	return text
}

// wallet functions
func action(text string) {

	sep := " "
	command, value, found := strings.Cut(text, sep)
	switch command {
	case "help":
		help()
	case "options":
		options()
	case "open":
		open(value, found)
		checkWallet()
		// Start the updates
		if !updating {
			go updater()
		}
	case "check":
		if dero.Wallet != nil {
			checkWallet()
		} else {
			println("No wallet open, type help for more info")
		}
	case "password":
		changePassword()
	case "seed":
		if checkPass(getText(`Enter Password:`)) {
			showSeed(dero.Wallet)
		} else {
			fmt.Println("Incorrect Password")
		}
	case "new":
		_, _, ok := createWallet(getText(`Enter Password:`))
		if ok {
			open(dero.WalletName, true)
			checkWallet()
		}
	case "recover":
		if value == "seed" {
			if recoverFromSeed() {
				checkWallet()
			}
		} else if value == "hex" {
			if recoverFromHex() {
				checkWallet()
			}
		}
	case "i8address":
		makeIntegratedAddress()
	case "send":
		if !found || value == "" {
			sendDero()
		} else if value == "token" {
			sendToken("")
		} else if len(value) > 60 {
			sendToken(value)
		}
	case "proof":
		getProof()
	case "txlist":
		listTxs()
	case "comments":
		showComments(command, value)
	case "tokens":
		tokens()
	case "clear":
		if value == "accounts" {
			cleanWallet(dero.Wallet)
		}
	case "close":
		close()
	case "exit":
		exit()

	// Gnomon
	case "start":
		startGnomon()
	case "pause":
		daemon.Pause()
	case "resume":
		daemon.UnPause()
	case "mute":
		gnomon_updates_muted = true
	case "unmute":
		gnomon_updates_muted = false
	case "indexes":
		getTelaIndexes()
	case "search":
		searchFiltered()
	// XSWD
	case "xswd":
		toggleXSWD()
	case "applications":
		showXSWDApps()
	}

	// Extend session if logged in
	if !sessionExpired() {
		sessionUpdate()
	}

	go input()
}

func help() {
	fmt.Print(`- HELP -

RPC SETTINGS:

-CLI-ARGS-
--rpc-login - Use "--rpc-login=user:pass" to rpc
--simulator - Enable simulator mode
--testnet - Enable testnet mode

COMMANDS:

options - Miscellaneous settings / procedures

-ACCOUNT-
new - Create new Dero wallet
open - Example: open wallet.db
check - Check registration status, show account etc
password - Change wallet password
seed - Displays wallet seed
recover seed - Recover from 25 seed phrase
recover hex - Recover from seed 64 char hex
close - Closes wallet
exit - Exits program

-TRANSACT-
send - Send Dero / enter integrated address
send token - Send a token
i8address - Make an integrated address
tokens - Scan for tokens
clear accounts - Clears wallet's saved token balances

-TOOLS-
proof - Enter a TX to get the proof if available
txlist - Show all TXs
comments - Show all comments
comments incoming - Show incoming comments
comments outgoing - Show outgoing comments

-GNOMON-
start - Run once
pause
resume
mute - Don't receive any Gnomon updates
unmute
indexes - Shows Tela indexes
search - Search filtered classes and tags

-XSWD-
xswd - Start / stop toggle for XSWD server
applications - List apps with access

`)
	result := getText(`Enter y to continue:`)
	if result == "y" {
		return
	}
}

func options() {
	option := getText(`
Edit Options

These setting apply to this session
[1]  Session expiration
[2]  Show / hide password entry
[3]  Edit wallet connection (applies next open)

Gnomon advanced controls / options
[10] Gnomon filter configuration
[11] Run filter reclassification task 
[12] Edit Gnomon http connections
[13] Max ram usage
[14] Show Gnomon status
[0]  Return

`)
	switch option {
	case "0":
		println("Returning")
		return
	case "1":
		t, _ := strconv.Atoi(getText("Enter session duration in minutes:"))
		if t != 0 {
			session_duration = time.Duration(t) * time.Minute
			println("Password prompt set to", t, "minute(s).")
		}
	case "2":
		setPassHidden()
	case "3":
		println("Current Daemon:", dero.DaemonAddr)
		dero.DaemonAddr = getText("Enter a daemon address or leave blank reset:")
	case "10":
		updateGnomonFilters()
	case "11":
		reclassify()
	case "12":
		updateGnomonConnections()
	case "13":
		updateGnomonMem()
	case "14":
		showGnomonStatus()
	}
	options()
}

func initializeGlobals() {
	rpc_login := flag.String("rpc-login", "", "string")
	testnet := flag.Bool("testnet", false, "bool")
	simulator := flag.Bool("simulator", false, "string")
	flag.Parse()
	// Set the Dero globals for rpc auth etc
	globals.Arguments["--rpc-login"] = *rpc_login
	globals.Arguments["--testnet"] = *testnet
	globals.Arguments["--simulator"] = *simulator

	fmt.Println("Arguments", globals.Arguments)
	globals.Initialize()
}
func open(name string, found bool) {
	dero.Path = getBasePath()
	if found && name != "" {
		dero.WalletName = name
	} else {
		dero.WalletName = getText(`Enter DB Name:`)
	}
	println("Opening", filepath.Join(dero.Path, dero.WalletName))
	pass := getPassword(`Enter Password:`)
	err := openWallet(pass)
	if err != nil {
		fmt.Println(err)
		return
	}
	println(dero.WalletName, "opened successfully")

	walletapi.Daemon_Endpoint = getDaemonAddress()
	common_processing(dero.Wallet)
	go walletapi.Keep_Connectivity() // maintain connectivity
	err = connectWallet()
	if err != nil {
		println("error:", err)
	}
	// disable gnomon updates when logged into wallet
	gnomon_updates_enabled = false
}
func getDaemonAddress() string {
	endpoint := "127.0.0.1:10102"
	endpoint_r := "node.derofoundation.org:11012"

	if !globals.IsMainnet() {
		endpoint = "testnetexplorer.dero.io:40402"
		if globals.IsSimulator() {
			endpoint = "127.0.0.1:40402"
		}
	}
	if globals.IsSimulator() {
		endpoint = "127.0.0.1:20000"
	}

	if dero.DaemonAddr == "" {
		dero.DaemonAddr = getText(`Enter wallet daemon endpoint, leave blank for default ("` + endpoint + `"), enter "r" for default remote endpoint ("` + endpoint_r + `")  :`)
		if dero.DaemonAddr == "" {
			dero.DaemonAddr = endpoint
		} else if dero.DaemonAddr == "r" {
			dero.DaemonAddr = endpoint_r
		}
	}
	return dero.DaemonAddr
}
func close() {
	if dero.Wallet != nil {
		fmt.Println("Shutting down wallet services...")
		dero.Wallet.SetOfflineMode()
		dero.Wallet.Save_Wallet()
		dero.Wallet.Close_Encrypted_Wallet()
		dero.Wallet = nil
		dero.PassHash = [32]byte{}
		fmt.Println("Wallet Closed...")
		if dero.RPC != nil {
			dero.RPC.RPCServer_Stop()
			dero.RPC = nil
			fmt.Println("RPC Closed...")
		}
		if dero.XSWD != nil {
			toggleXSWD()
		}
		if gnomon.TargetHeight != 0 && !daemon.Paused() && !gnomon_updates_muted {
			fmt.Println("Resuming Gnomon status updates.")
		}
		gnomon_updates_enabled = true
	}
}

func exit() {
	close()
	fmt.Println("Exiting...")
	os.Exit(0)
}

// sets online mode, starts RPC server etc
func common_processing(wallet *walletapi.Wallet_Disk) {
	fmt.Println("Setting online mode")
	wallet.SetOnlineMode()
	//wallet.SetTrackRecentBlocks(1000000)
	if wallet.SetTrackRecentBlocks(-1) == 0 {
		fmt.Println("Wallet will track entire history")
	} else {
		fmt.Println("Wallet will track recent blocks", "blocks", wallet.SetTrackRecentBlocks(-1))
	}
	//	wallet.SetSaveDuration(time.Duration(s) * time.Second)
	wallet.SetSaveDuration(-1)
	wallet.SetNetwork(!globals.Arguments["--testnet"].(bool))

	rpc_enabled := false
	if globals.Arguments["--rpc-login"] != nil {
		userpass := globals.Arguments["--rpc-login"].(string)
		parts := strings.SplitN(userpass, ":", 2)
		if len(parts) == 2 {
			rpc_enabled = true
			println("Wallet RPC", "username", parts[0], "password", parts[1])
		}
	}

	// start rpc server if requested
	if rpc_enabled {
		var err error
		if dero.RPC, err = rpcserver.RPCServer_Start(wallet, "walletrpc"); err != nil {
			fmt.Println(err, "Error starting rpc server")
		} else {
			rpc_port := config.Mainnet.Wallet_RPC_Default_Port
			if globals.Arguments["--testnet"].(bool) {
				rpc_port = config.Testnet.Wallet_RPC_Default_Port
			}
			fmt.Println("RPC Started at 127.0.0.1:" + strconv.Itoa(rpc_port))
		}
	}
	time.Sleep(time.Second)
}

// Command
// --rpc-login=derouser:pass
// Enables rpc with password
// Endpoint
// curl -X POST "http://127.0.0.1:10103/json_rpc" -H "Content-Type: application/json" -u "derouser:password" -d "{\"jsonrpc\":\"2.0\",\"id\":\"1\",\"method\":\"GetAddress\"}"

// XSWD Functions
func toggleXSWD() {
	if dero.XSWD != nil {
		dero.XSWD.Stop()
		dero.XSWD = nil
		return
	}

	// NewXSWDServer default behavior is to Ask permission for all requests
	dero.XSWD = xswd.NewXSWDServer(dero.Wallet, func(app *xswd.ApplicationData) (a bool) {
		// xswd logger informs if app is requesting permissions upon connection or if app is already connected
		fmt.Println("New XSWD permission request, hit enter to continue")
		inputmsg = fmt.Sprintf("Allow application %s (%s) to access your wallet (y/n): ", app.Name, app.Url)
		// clear current cursor
		result := getText("")
		inputmsg = ""
		if result == "y" {
			fmt.Println("Allowing app access...")
			keyInput = ""
			return true
		}
		keyInput = ""
		return false
	}, func(app *xswd.ApplicationData, request *jrpc2.Request) (perm xswd.Permission) {

		method := request.Method()
		param := strings.ReplaceAll(strings.Join(strings.Fields(request.ParamString()), " "), "\n", " ")

		//	values := []string{"A", "D", "AA", "AD"}
		prompt := fmt.Sprintf("Request from %s: %s | Params: %s | Do you want to allow this request ? ([A]llow / [D]eny / [AA] Always Allow / [AD] Always Deny): ", app.Name, method, param)
		if !dero.XSWD.CanStorePermission(method) {
			//values = []string{"A", "D", "AD"}
			prompt = fmt.Sprintf("Request from %s: %s | Params: %s | Do you want to allow this request ? ([A]llow / [D]eny / [AD] Always Deny): ", app.Name, method, param)
		}
		fmt.Println("New XSWD permission request, press Enter to continue") //cancels out current key listener and allows getText to run next
		input := make(chan string)
		go func() {
			//clear current cursor
			inputmsg = prompt
			result := strings.ToUpper(string(getText("")))
			//Reset custom prompt message
			inputmsg = ""
			result = strings.ToUpper(string(result))
			input <- result
		}()
		for {
			select {
			case <-app.OnClose:
				println("App closing and denying")
				return xswd.Deny

			case line := <-input:

				line = strings.ToUpper(strings.TrimSpace(line))
				perm := xswd.Deny
				println("Applying Permission:" + line)
				if line == "A" {
					perm = xswd.Allow
				} else if line == "AA" {
					perm = xswd.AlwaysAllow
				} else if line == "AD" {
					perm = xswd.AlwaysDeny
				}
				showXSWDApps()
				return perm
			}
		}
	})

	// check if start was successful
	time.Sleep(time.Second)
	if !dero.XSWD.IsRunning() {
		dero.XSWD = nil
	}
}

func showXSWDApps() {
	if dero.XSWD == nil {
		fmt.Println(nil, "XSWD server is not running")
		return
	}
	apps := dero.XSWD.GetApplications()
	fmt.Println(fmt.Sprintf("XSWD Applications (%d):", len(apps)))
	for _, app := range apps {
		fmt.Println("Application", "id", app.Id, "name", app.Name, "description", app.Description, "url", app.Url)
		for name, perm := range app.Permissions {
			fmt.Println(fmt.Sprintf("Permission %s", app.Name), name, perm)
		}

		for event, sub := range app.RegisteredEvents {
			fmt.Println(fmt.Sprintf("Subscribed %s", app.Name), string(event), sub)
		}
	}

}

/*
type Commands struct {
	Gnomon interface {
		startGnomon()
		pause()
		unPause()
	}
}	Commands.Gnomon.startGnomon()
*/
// Create config
var GConfig = gnomon.Configuration{
	RamSizeMB:   0, //default 0 gigs until set
	SpamLevel:   "0",
	Smoothing:   0,
	DisplayMode: -1, // Enables event channel
	Filters: map[string]map[string][]string{
		"g45": {
			"tags":    {"G45-AT", "G45-C", "G45-FAT", "G45-NAME", "T345"},
			"options": {"i"}, //regex filters for word boundry and c.i. matching (supports: "b", "i"), "co" saves class only
		},
		"nfa":   {"tags": {"ART-NFA-MS1"}},
		"swaps": {"tags": {"StartSwap"}},
		"tela":  {"tags": {"docVersion", "telaVersion"}},
		"token": {
			"tags":    {"SEND_ASSET_TO_ADDRESS"},
			"options": {"i", "co"}, //co saves class only
		},
	},
	Endpoints: []daemon.Connection{
		{Address: "dero-node-ch4k1pu.mysrv.cloud"},
		{Address: "64.226.81.37:10102"}, //dero-node.net
		//	{Address: "51.81.96.25:10102"},//dero.geeko.cloud
		//173.208.130.94:11012
	},
	//Port        string
	CmdFlags: map[string]any{ //Override the cmd flags with desired usage
		"port":      "0",       //use 0 for none
		"mode":      "mainnet", //"mainnet" or "testnet"
		"simulator": false,     //bool
	},
}

// Gnomon setup and start
func startGnomon() {

	if globals.Arguments["--testnet"].(bool) {
		GConfig.CmdFlags["mode"] = "testnet"
	}
	if globals.Arguments["--simulator"].(bool) {
		GConfig.CmdFlags["simulator"] = true
	}
	GConfig.RamSizeMB = getGnomonMaxMem()              //pass in the defaults
	gnomon.Filters = getGnomonFilters(GConfig.Filters) //pass in the defaults
	fmt.Println("Gnomon using up to", GConfig.RamSizeMB, "MB of ram")
	// Start Gnomon
	go func() {
		gnomon.Start(GConfig, getGnomonConnections())
	}()

}

func showGnomonStatus() {
	if gnomon.TargetHeight == 0 {
		fmt.Println("Gnomon not started.")
	} else {
		if daemon.Paused() {
			fmt.Println("Gnomon paused.")
		} else {
			fmt.Println("Gnomon running.")
		}
	}
	fmt.Println("Using memory (gnomon):", gnomon.UseMem)
	fmt.Println("Max memory usage (gnomon):", gnomon.RamSizeMB, "MB")
	fmt.Println("Max memory usage (saved):", getGnomonMaxMem(), "MB")
	db_name := fmt.Sprintf("sql%s.db", "GNOMON")
	db_path := filepath.Join(GConfig.CmdFlags["mode"].(string), "gnomondb")
	fmt.Println("File location:", filepath.Join(db_path, db_name))
	fmt.Println("Size on disk:", fileSizeMB(filepath.Join(db_path, db_name)), "MB")
	Sqlite := getGnomonDiskDB()
	defer Sqlite.DB.Close()
	highest_indexed, err := Sqlite.GetLastIndexHeight()
	if err != nil {
		fmt.Println("Error getting highest index:", highest_indexed)
	} else {
		fmt.Println("Highest indexed block:", highest_indexed)
	}
	if gnomon.TargetHeight != 0 {
		fmt.Println("Filters applied:", len(gnomon.Filters))
	} else {
		val, _ := Sqlite.LoadSetting("Filters")
		if val != "" {
			var f map[string]map[string][]string
			json.Unmarshal([]byte(val), &f)
			fmt.Println("Filters applied:", f)
		}
	}
	val, _ := Sqlite.LoadSetting("completed")
	if val != "" {
		var c [][2]int
		json.Unmarshal([]byte(val), &c)
		total := 0
		for i, completed := range c {
			total += completed[1] - completed[0]
			if i == len(c)-1 {
				total += int(highest_indexed - int64(completed[1]))
			}
		}
		fmt.Println("Number of blocks indexed:", total)
		topoheight := gnomon.LatestTopoHeight
		if topoheight == 0 {
			if dero.Wallet != nil {
				if dero.Wallet.IsDaemonOnlineCached() {
					topoheight = int64(dero.Wallet.Get_Daemon_Height())
				} else {
					topoheight = int64(dero.Wallet.Get_Height())
				}
			} ///try another way
		}
		if topoheight != 0 {
			fmt.Println("Progress: ", fmt.Sprintf("%.2f", 1.0/(float64(topoheight)/float64(total))*100.0), "%")
		}
	}
}

func getGnomonDiskDB() (Sqlite *sql.SqlStore) {
	// gnomon uses dbPathAndName()
	db_name := fmt.Sprintf("sql%s.db", "GNOMON")
	db_path := filepath.Join(GConfig.CmdFlags["mode"].(string), "gnomondb")
	Sqlite, _ = sql.NewDiskDB(db_path, db_name)
	var exists int
	Sqlite.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='state');").Scan(&exists)
	if exists == 0 {
		sql.CreateTables(Sqlite.DB)
	}
	return
}

// Gets saved mem setting if available
func getGnomonMaxMem() (maxram int) {
	Sqlite := getGnomonDiskDB()
	defer Sqlite.DB.Close()
	val, _ := Sqlite.LoadSetting("RamSizeMB")
	var err error
	if val != "" {
		maxram, err = strconv.Atoi(val)
		if err != nil {
			maxram = 0
		}
	}
	return
}
func updateGnomonMem() {
	Sqlite := getGnomonDiskDB()
	defer Sqlite.DB.Close()
	meminmb, _ := Sqlite.LoadSetting("RamSizeMB")
	if meminmb != "" {
		fmt.Println("Current setting in Mb:", meminmb)
	}
	meminmb = getText("Enter system memory to allow Gnomon to use in Megabytes.")
	fmt.Println("Saving value in Mb:", meminmb)
	Sqlite.SaveSetting("RamSizeMB", meminmb)
	//Should switch to disk mode if set under the file size on the next batch finish
	if gnomon.TargetHeight != 0 {
		fmt.Println("Updating live settings (max ram Mb). This doesn't free up ram immediately and requires a restart to enable in-memory tables.", meminmb)
		gnomon.RamSizeMB, _ = strconv.Atoi(meminmb)
	}
}

// Gets saved connections if available
func getGnomonConnections() (endpoints []daemon.Connection) {
	Sqlite := getGnomonDiskDB()
	defer Sqlite.DB.Close()
	val, _ := Sqlite.LoadSetting("Endpoints")
	if val != "" {
		endpoints = []daemon.Connection{}
		addrs := strings.Split(val, ",")
		for _, add := range addrs {
			endpoints = append(daemon.Endpoints, daemon.Connection{Address: strings.TrimSpace(add)})
		}
	}
	return
}

// Update connections or set to empty if and use defaults in the config
func updateGnomonConnections() {
	Sqlite := getGnomonDiskDB()
	defer Sqlite.DB.Close()
	val, _ := Sqlite.LoadSetting("Endpoints")
	var endpoints = GConfig.Endpoints
	if getText("Reset to defaults? (y/n)") != "y" {
		if val != "" {
			fmt.Println("Saved value:", val)
		} else {
			fmt.Println("Default value:", endpoints)
		}
		addrs := val
		if getText("Update Connections with new csv? (y/n)") == "y" {
			addrs = getText("Enter new connections csv (eg. node.derofoundation.org:10102):")
			endpoints = []daemon.Connection{}
			addrslist := strings.Split(addrs, ",")
			//maybe test here...
			for _, add := range addrslist {
				endpoints = append(endpoints, daemon.Connection{Address: strings.TrimSpace(add)})
			}
		}

		fmt.Println("Saving:", addrs)
		Sqlite.SaveSetting("Endpoints", addrs)
	} else if val != "" {
		fmt.Println("Using defaults.")
		Sqlite.SaveSetting("Endpoints", "")
	}
}

// Gnomon filters done before startup for now
func reclassify() {
	if gnomon.TargetHeight != 0 {
		println("Restart wallet and run before starting Gnomon.")
	}
	autostart := false
	res := getText("Start Gnomon after reclassification task has finished? (y/n)")
	if res == "y" {
		autostart = true
	}
	gnomon.RamSizeMB = getGnomonMaxMem()
	gnomon.UseMem = true

	//maybe check size here
	db_name := fmt.Sprintf("sql%s.db", "GNOMON")
	db_path := filepath.Join(GConfig.CmdFlags["mode"].(string), "gnomondb")
	if fileSizeMB(filepath.Join(db_path, db_name)) > int64(gnomon.RamSizeMB) {
		gnomon.UseMem = false
		fmt.Println("Using up disk mode. This could take a while...")
	} else {
		fmt.Println("Using up to", gnomon.RamSizeMB, "MB of ram.")
	}
	if getText("Continue? (y/n)") != "y" {
		return
	}
	fmt.Println("Loading...")
	gnomon.Sqlite, _ = sql.NewSqlDB(db_path, db_name)
	gnomon.Filters = getGnomonFilters(GConfig.Filters)
	gnomon.InitializeFilters()
	gnomon.ReClassify()
	if autostart && gnomon.TargetHeight == 0 {
		startGnomon()
	}
}

// also in gnomon.go
func fileSizeMB(filePath string) int64 {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return 0
	}
	sizeBytes := fileInfo.Size()
	return int64(float64(sizeBytes) / (1024 * 1024))
}

// Gnomon filters done before startup for now
func updateGnomonFilters() {
	Sqlite := getGnomonDiskDB()
	defer Sqlite.DB.Close()
	var val string
	if getText("Reset to default filters? (y/n)") != "y" {
		updates_enabled = false
		val, _ = Sqlite.LoadSetting("Filters")
		GConfig.Filters = gnomon.EditFilters(GConfig.Filters)
		updates_enabled = true
	}
	bytes, _ := json.Marshal(GConfig.Filters)
	val = string(bytes)
	Sqlite.SaveSetting("Filters", val)
}

// Gets saved filters if available
func getGnomonFilters(Filters map[string]map[string][]string) map[string]map[string][]string {
	Sqlite := getGnomonDiskDB()
	defer Sqlite.DB.Close()
	val, _ := Sqlite.LoadSetting("Filters")
	if val != "" {
		var temp map[string]map[string][]string
		json.Unmarshal([]byte(val), &temp)
		Filters = temp
	}
	return Filters
}

// Runs after startup
func getTelaIndexes() {
	if gnomon.TargetHeight == 0 {
		println("Gnomon not started")
		return
	}
	fmt.Println("Showing Tela Indexes")
	scids := gnomon.Sqlite.GetSCIDsByTags([]string{"telaVersion"})
	height, _ := gnomon.Sqlite.GetLastIndexHeight()
	fmt.Println("Checking at Height:", height)
	for _, scid := range scids {
		fmt.Println("")
		fmt.Println("Tela Index SCID:", scid)

		var hVars []*structs.SCIDVariable
		hVars = gnomon.Sqlite.GetSCIDVariableDetailsAtTopoheight(scid, height)

		for _, variable := range hVars {
			if variable.Key == "nameHdr" ||
				variable.Key == "iconURLHdr" ||
				variable.Key == "descrHdr" {
				fmt.Println(variable.Key, ":", variable.Value)
			}
		}
	}
}

// Returns a list of distinct values from CSV results returned via the query
func getDistinctFromCSV(q string, v any) (results []string) {
	var list string
	//sql.Ask() //applies for disk mode
	sql.SetReady(false)
	rows, err := gnomon.Sqlite.DB.Query(q, v)
	sql.SetReady(true)
	if err != nil {
		fmt.Println(err)
	}
	for rows.Next() {
		rows.Scan(&list)
		items := strings.Split(list, ",")
		//	fmt.Println(items)
		for _, item := range items {
			if !slices.Contains(results, item) {
				results = append(results, item)
			}
		}
	}
	return
}

// Search for scs by class or tag
func searchFiltered() {
	if gnomon.TargetHeight == 0 {
		println("Gnomon not started")
		return
	}
	scids := []string{}
	address := ""
	results := []string{}
	kind := getText(`Enter "c" for class or "t" for tags or "a" to search by address, blank to use your address`)
	if kind == "c" {
		results = getDistinctFromCSV(`SELECT DISTINCT class FROM scs`, nil)
		fmt.Println("Classes currently in DB:")

	} else if kind == "t" {
		results = getDistinctFromCSV(`SELECT DISTINCT tags FROM scs`, nil)
		fmt.Println("Tags currently in DB:")

	} else if kind == "a" {
		address = getText(`Enter Dero wallet address:`)
	} else if dero.Wallet != nil {
		address = dero.Wallet.GetAddress().String()
	} else {
		println("No open wallet or address provided.")
		return
	}

	if kind == "c" || kind == "t" {
		for _, item := range results {
			fmt.Println(item)
		}
	}
	if kind == "c" {
		scids = gnomon.Sqlite.GetSCIDsByClass(strings.Split(getText(`Enter class or classes csv to search for:`), ","))
	} else if kind == "t" {
		scids = gnomon.Sqlite.GetSCIDsByTags(strings.Split(getText(`Enter tag or tags csv to search for:`), ","))
	} else if address != "" {
		//get the results for address search
		for scid, scowner := range gnomon.Sqlite.GetAllOwnersAndSCIDs() {
			if scowner == address {
				scids = append(scids, scid)
			}
		}
	}
	scidcount := len(scids)
	fmt.Println("Number of results:", len(scids))
	if scidcount == 0 {
		return
	}

	max, _ := strconv.Atoi(getText(`Enter max number of results to show: `))
	show_details := false
	if getText(`Show details (can be slow for large sets using disk mode)? (y/n)`) == "y" {
		show_details = true
	}
	start := scidcount - max
	start = int(math.Max(0, float64(start)))
	height, _ := gnomon.Sqlite.GetLastIndexHeight()
	fmt.Println("Checking at Height:", height)
	for si, scid := range scids {
		if si < start {
			continue
		}
		fmt.Println("")
		fmt.Println("SCID:", scid, "-------------------")
		if show_details {
			var hVars []*structs.SCIDVariable
			hVars = gnomon.Sqlite.GetSCIDVariableDetailsAtTopoheight(scid, height)
			for i, variable := range hVars {
				isstr := false
				switch variable.Value.(type) {
				case string:
					isstr = true
				}

				if isstr && strings.Contains(variable.Key.(string), "name") {
					fmt.Println(variable.Key, ":", variable.Value)
				} else {
					if isstr {
						if len(variable.Value.(string)) > 100 {
							fmt.Println(variable.Key, ":", strings.TrimSpace(variable.Value.(string)[:100]), "...")
						} else {
							fmt.Println(variable.Key, ":", strings.TrimSpace(variable.Value.(string)))
						}
					} else {
						fmt.Println(variable.Key, ":", variable.Value)
					}
				}
				if i > 3 {
					break
				}
			}
		}
		fmt.Println("------------------------------------------------------------------------------------------")
	}
	getText(`Press enter to continue.`)
}

/*
// Search for scs by tag
func searchByClass() {
	tag := getText("Enter tag or csv of tags")
	tags := strings.Split(tag, ",")
	scids := gnomon.Sqlite.GetSCIDsByTags(tags)
	height, _ := gnomon.Sqlite.GetLastIndexHeight()
	fmt.Println("Checking at Height:", height)
	for _, scid := range scids {
		fmt.Println("")
		fmt.Println("SCID:", scid)
		var hVars []*structs.SCIDVariable
		hVars = gnomon.Sqlite.GetSCIDVariableDetailsAtTopoheight(scid, height)
		for _, variable := range hVars {
			if variable.Key == "nameHdr" {
				fmt.Println("-------------------")
				fmt.Println(variable.Key, ":", variable.Value)
				fmt.Println("-------------------")
			} else {
				fmt.Println(variable.Key, ":", variable.Value)
			}
		}
	}
}
*/
