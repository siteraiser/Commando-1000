package gnomon

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gnomon/api"
	"gnomon/daemon"
	sql "gnomon/db"
	"gnomon/show"
)

type Configuration struct {
	RamSizeMB   int
	SpamLevel   string
	Smoothing   int
	DisplayMode int
	Filters     map[string]map[string][]string
	Endpoints   []daemon.Connection
	Port        string
	CmdFlags    map[string]any
}

/* CUSTOM FILTERS */
// "tela" is a class and "docVersion" and "telaVersion" are the tags
// Gnomon will search for the tags and save the class and tags when the SC contains a match

var regexes = map[string]string{}
var Filters map[string]map[string][]string
var defaultFilters = map[string]map[string][]string{
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
}

func dbPathAndName() (db_path string, db_name string) {
	db_name = fmt.Sprintf("sql%s.db", "GNOMON")
	wd := GetDirectory()
	db_path = filepath.Join(wd, "gnomondb")
	return
}
func initDB() {
	//Create the tables now...
	Sqlite, _ := sql.NewDiskDB(dbPathAndName())
	sql.CreateTables(Sqlite.DB)
	Sqlite.DB.Close()
}

func setFlags() {
	//if the port is set then launch the server
	portFlag := flag.Int("port", 0000, "string")
	envModeFlag := flag.String("mode", "mainnet", "string")
	simulatorFlag := flag.Bool("simulator", false, "string")
	flag.Parse()
	Config.CmdFlags["port"] = strconv.Itoa(*portFlag)
	Config.CmdFlags["mode"] = *envModeFlag
	Config.CmdFlags["simulator"] = *simulatorFlag
}

// Do something similar to what the derohe globals does
// tells whether we are in mainnet mode
// if we are not mainnet, we are a testnet,
// we will only have a single mainnet ,( but we may have one or more testnets )
func isMainnet() bool {
	return Config.CmdFlags["mode"] == "mainnet"
}

// tells whether we are in simulator mode ( both mainnet and testnet coud be simulated)
func isSimulator() bool {
	if Config.CmdFlags["simulator"] != nil && Config.CmdFlags["simulator"].(bool) == true {
		return true
	}
	return false
}

// return different directories for different networks ( mainly mainnet, testnet, simulation )
// this function is specifically for daemon
func GetDirectory() string {
	data_directory, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error obtaining current directory, using temp dir err %s\n", err)
		data_directory = os.TempDir()
	}

	simulator := ""
	if isSimulator() {
		simulator = "_simulator" // add _simulator
	}

	if isMainnet() {
		return filepath.Join(data_directory, "mainnet"+simulator)
	}

	return filepath.Join(data_directory, "testnet"+simulator)
}

func getConfig(update bool) Configuration {
	config := Config
	var text string
	Sqlite, _ := sql.NewDiskDB(dbPathAndName())
	defer Sqlite.DB.Close()
	// Ram settings
	val, err := sql.LoadSetting(Sqlite.DB, "RamSizeMB")
	if val == "" || update {
		print("Enter system memory to use in GB(0,2,8,...):")
		_, err = fmt.Scanln(&text)
		config.RamSizeMB, _ = strconv.Atoi(text)
		config.RamSizeMB *= int(1024)
		if err != nil {
			println("Error:", err)
			panic("Use integer for value")
		}
		sql.SaveSetting(Sqlite.DB, "RamSizeMB", strconv.Itoa(config.RamSizeMB))
	} else {
		print(val, "MB of ram being used")
		config.RamSizeMB, _ = strconv.Atoi(val)
	}

	// Smoothing settings
	val, err = sql.LoadSetting(Sqlite.DB, "Smoothing")
	if val == "" || update {
		print("Use smoothing? 0-1000:")
		_, err = fmt.Scanln(&text)
		if err != nil {
			println("Error:", err)
			panic("Use integer for value")
		}
		sql.SaveSetting(Sqlite.DB, "Smoothing", text)
	} else {
		println("Using Saved Smoothing Period:", val)
		config.Smoothing, _ = strconv.Atoi(val)
	}

	// Spam settings
	val, err = sql.LoadSetting(Sqlite.DB, "SpamLevel")
	if val == "" || update {
		println("SC spam threshold 0-50 recommended")
		print("Enter number of name registrations allowed per wallet:")
		_, err = fmt.Scanln(&text)
		if err != nil {
			println("Error:", err)
			panic("Use integer for value")
		}
		config.SpamLevel = text
		sql.SaveSetting(Sqlite.DB, "SpamLevel", text)
	} else {
		println("Using Smoothing Level: ", val)
		config.SpamLevel = val
	}

	// Endpoint/daemon connections
	val, err = sql.LoadSetting(Sqlite.DB, "Endpoints")
	if val == "" || update {
		println("Enter custom connection or enter n to use the default remote connections eg. node.derofoundation.org:10102 ")
		_, err = fmt.Scanln(&text)
		if text != "n" {
			daemon.Endpoints = []daemon.Connection{}
			addrs := strings.Split(text, ",")
			for _, add := range addrs {
				daemon.Endpoints = append(daemon.Endpoints, daemon.Connection{Address: strings.TrimSpace(add)})
			}
			sql.SaveSetting(Sqlite.DB, "Endpoints", text)
		} else {
			defaultAddress := ""
			if !isMainnet() {
				defaultAddress = "testnetexplorer.dero.io:40402"
				if isSimulator() {
					defaultAddress = "127.0.0.1:40402"
				}
			}
			if isSimulator() {
				defaultAddress = "127.0.0.1:20000"
			}
			//Overrides default daemons with specific
			if defaultAddress != "" {
				daemon.Endpoints = []daemon.Connection{
					{Address: defaultAddress},
				}
			}
			sql.SaveSetting(Sqlite.DB, "Endpoints", "")
		}
	} else {
		println("Using Connections: ", val)
		daemon.Endpoints = []daemon.Connection{}
		addrs := strings.Split(val, ",")
		for _, add := range addrs {
			daemon.Endpoints = append(daemon.Endpoints, daemon.Connection{Address: strings.TrimSpace(add)})
		}
		config.Endpoints = daemon.Endpoints
	}

	// Filters

	val, err = sql.LoadSetting(Sqlite.DB, "Filters")
	if val == "" || update {
		println("Edit filters? y or n")
		_, err = fmt.Scanln(&text)
		if text == "n" {
			if val == "" {
				Filters = defaultFilters
			} else {
				var temp map[string]map[string][]string
				json.Unmarshal([]byte(val), &temp)
				Filters = temp
			}
		} else if text == "y" {
			var temp map[string]map[string][]string
			if val == "" {
				Filters = EditFilters(defaultFilters)
				bytes, _ := json.Marshal(Filters)
				val = string(bytes)
				sql.SaveSetting(Sqlite.DB, "Filters", val)
			} else {
				json.Unmarshal([]byte(val), &temp)
				Filters = EditFilters(temp)
				bytes, _ := json.Marshal(Filters)
				val = string(bytes)
				sql.SaveSetting(Sqlite.DB, "Filters", val)
			}

		}
	} else {
		if val != "" {
			var temp map[string]map[string][]string
			json.Unmarshal([]byte(val), &temp)
			Filters = temp
		} else {
			Filters = defaultFilters
		}
	}

	if Config.CmdFlags["port"] != "0" {
		go api.Start(Config.CmdFlags["port"].(string), GetDirectory())
	} else {
		//ask
		fmt.Println("Enter a port number for the api or n to skip:")
		_, err = fmt.Scanln(&text)
		if _, err := strconv.Atoi(text); err == nil && text != "n" {
			go api.Start(text, GetDirectory())
			time.Sleep(200 * time.Millisecond)
		}
	}

	fmt.Println("Choose display mode, 0, 1 or 2:")
	_, err = fmt.Scanln(&text)
	show.DisplayMode, _ = strconv.Atoi(text)

	return config
}

func EditFilters(filters map[string]map[string][]string) map[string]map[string][]string {
	fmt.Println("-- Filters: ")
	for class, filter := range filters {
		fmt.Println("Class:", class, "----------------------------------------------------")
		fmt.Println("Filter:", filter)
	}
	fmt.Println("--------------------------------------------")
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(`Type the name of the class of filter to edit or "add classname" or "delete classname" or "done" to return:`)
	text, err := reader.ReadString('\n')
	if err != nil {
		println("Error reading input:", err)
	}
	text = strings.TrimSpace(text)
	reader.Reset(os.Stdin)

	if text == "done" {
		return filters
	} else if len(text) > 3 && strings.Contains(text[:3], "add") {
		f := map[string][]string{}
		filters[text[4:]] = f
		return EditFilters(filters)
	} else if len(text) > 6 && strings.Contains(text[:6], "delete") {
		if _, exists := filters[text[7:]]; exists {
			delete(filters, text[7:])
			fmt.Printf("'%s' deleted.\n", text[7:])
		}
		return EditFilters(filters)
	}
	filters[text] = editFilter(filters[text])
	return EditFilters(filters)
}
func editFilter(filter map[string][]string) map[string][]string {
	var text string
	println("Enter 1 to edit filter or 2 for options:")
	_, _ = fmt.Scanln(&text)
	if text == "1" {
		filter = changeTags(filter)
	} else {
		filter = changeOption(filter)
	}
	return filter
}
func changeTags(filter map[string][]string) map[string][]string {
	fmt.Println("Current tags:", strings.Join(filter["tags"], ","))
	var text string
	println("Enter new csv list of tags or type done to return:")
	_, _ = fmt.Scanln(&text)
	if text != "done" {
		filter["tags"] = strings.Split(text, ",")
	}
	return filter
}
func changeOption(option map[string][]string) map[string][]string {
	fmt.Println("Current options:", option["options"])
	var text string
	println(`"i" is case-insensitive match and "b" is word boundry match.`)
	println(`Enter new csv list of options eg, "i,b", or "i" or type done to return:`)

	_, _ = fmt.Scanln(&text)
	if text != "done" {
		option["options"] = strings.Split(text, ",")
		if len(option["options"]) == 0 {
			delete(option, "options")
		}
	}
	return option
}

func updateCompleted(starting_height int64, lowest_daemon_height int64, completed string, start int, finish int) (string, int64, int64) {
	ending_height := int64(-1)
	var complete [][2]int
	json.Unmarshal([]byte(completed), &complete)

	// Add chunks
	if start != finish {
		if len(complete) == 0 {
			complete = append(complete, [2]int{start, finish})
		} else {
			for i, _ := range complete {
				if int(start) >= complete[i][0] && int(start) <= complete[i][1] {
					complete[i][1] = int(finish)
					break
				} else if int(start) > complete[i][1] || int(finish) == complete[i][0] {
					complete = append(complete, [2]int{start, finish})
					break
				} else if int(start) < complete[i][0] && int(finish) < complete[i][1] {
					var temp [][2]int
					temp = append(temp, [2]int{start, finish})
					complete = append(temp, complete...)
					break
				}
			}
			//may need to skip if start and finish are the same...
		}
		//check for 0 len chunks and remove
		for i, _ := range complete {
			if len(complete) > i+1 {
				if complete[i][0] == complete[i][1] {
					complete = append(complete[:i], complete[i+1:]...)
					break
				}
			}
		}
	}

	sort.Slice(complete, func(i, j int) bool {
		return complete[i][0] < complete[j][0]
	})

	// Merge the chunks
	if len(complete) > 1 {
		var newcomplete = [][2]int{}
		for i, chunk := range complete {
			if len(complete) > i+1 {
				if len(newcomplete) == 0 {
					newcomplete = append(newcomplete, [2]int{chunk[0], chunk[1]})
				}
				if newcomplete[len(newcomplete)-1][1] == complete[i+1][0] {
					newcomplete[len(newcomplete)-1][1] = complete[i+1][1]
				} else {
					newcomplete = append(newcomplete, [2]int{complete[i+1][0], complete[i+1][1]})
				}
			}
		}
		complete = newcomplete
	}

	if len(complete) != 0 {
		lastend := int64(0)
		found := false
		for i, _ := range complete {
			if lowest_daemon_height < int64(complete[i][0]) {
				found = true
				if lastend == 0 { //first one...
					starting_height = lowest_daemon_height
					ending_height = int64(complete[i][0])
					break
				} else if lowest_daemon_height <= lastend {
					starting_height = lastend
					ending_height = int64(complete[i][0])
					break
				} else {
					starting_height = lowest_daemon_height
					ending_height = int64(complete[i][0])
					break
				}
			}
			lastend = int64(complete[i][1])
		}
		if !found && int64(complete[len(complete)-1][1]) >= lowest_daemon_height {
			starting_height = int64(complete[len(complete)-1][1])
			ending_height = -1
		}
	}
	// If preferred starting height falls in middle of chunk (should probably be equal to etc..), move forward to end of chunk to earliest slot that needs filling
	for _, chunk := range complete {
		if chunk[0] > int(starting_height) && chunk[1] <= int(starting_height) && starting_height >= lowest_daemon_height {
			starting_height = int64(chunk[1]) //-1
			break
		}
	}
	// Set ending height to start of next completed chunk
	for _, chunk := range complete {
		if starting_height < int64(chunk[0]) {
			ending_height = int64(chunk[0])
			break
		}
	}
	res, _ := json.Marshal(complete)
	return string(res), starting_height, ending_height
}
